use tokio::sync::broadcast::{
    error::RecvError as BroadcastRVError,
};
use tokio::sync::oneshot::{
    Receiver,
};
use yrs::updates::encoder::Encode;
use std::time::Instant;
use tokio_util::sync::CancellationToken;
use uuid::Uuid;
use axum::{
    extract::ws::{
        Message, 
        WebSocket, 
        CloseFrame,
    },
};
use futures_util::{
    SinkExt, 
    stream::{SplitSink},
};
use yrs::{
    StateVector, Update,
    sync::protocol::SyncMessage,
    updates::decoder::Decode,
};
use crate::broker::{Routable, WrappedReceiver};
use crate::repository::{Repository};
use crate::state_machine::{Writer, WriterAwaitingHandshake, WriterHotPath};
use tracing::{Instrument, Level, event, info_span, instrument};


use crate::handlers::{
    WebsocketHandler,
    TaskError,
    ReaderEvent,
    UpdateMessage,
};

// TODO: these logs should be part of the instrumentation for server sync step one messages
// verify that they are part of the server sync step one instrumentation
fn build_server_sync_step_1(
    last_received_offset: Option<u32>, client_id: u64,
) -> SyncMessage {
    /*
    Originally this code was part of the reader to preserve separation of concerns. The read task is 
    concerned with which operations the server has received from the client. The writer is concerned with 
    which operations the client has received from the server. This code was moved from the reader to the
    writer to simplify the implementation of the writer. If this message is created in the writer. It 
    does not need to be communicated between the reader and the writer. The writer implementation does
    not need to worry about listening for Server Sync Step 1 reader events when it is in the hot path.
    This eliminates the possibility of a race condition between the writer and the reader in which the
    writer stops listening for reader events (because it is in the hot path) before the reader has 
    the time to transmit the server sync step 1 message
    */
    /*
    In the future if we want to reinstate separation of concerns:
    - the server sync step one message should be returned by the read state machine constructor
    - Create the reader and the writer state machines in the handle_socket function.
    - pass the server sync step one message into the writer task along with the writer state machine
    */
    /*
    Tradeoff:
    - the writer task has to wait to construct and send a server sync step one message before it can 
      receive an process a client sync step one message. This means that potentially long database 
      read times could stall the writer handshake process even though reading the last received message
      for this client_id is independent from the writer sync process.
    */
    /*
    State vectors in Yrs are zero indexed, meaning that a state vector that has received no operations 
    from a client has no record of that client (instead of having the key value pair client_id: 0).
    For this reason we have to send an empty version vector if we have seen no operations from this client
     */
    // send a message to the client indicating what updates we have already so that the client can send us
    // a bulk update of messages that we are missing 
    let mut reader_sv = StateVector::default();
    if let Some(op) = last_received_offset {
        // yrs expects that the state vectors passed around include exclusive upper bounds
        // of operations received for a client_id instead of inclusive upper bounds.
        reader_sv.set_max(client_id, op + 1);
    }
    event!(
        Level::DEBUG,
        client_id_src=client_id,
        ?reader_sv,
        last_received_offset,
        "sending state vector from writer to reader",
    );
    SyncMessage::SyncStep1(reader_sv)
}

impl <R: Repository> WebsocketHandler<R> {
    #[instrument(skip_all)]
    async fn send_server_sync_step_one(
        &self,
        websocket_sender: &mut SplitSink<WebSocket, Message>,
    ) -> Result<(), TaskError> {
        let start = Instant::now();
        let mut _last_received_offset = self.repo.read_last_received_offset(
            self.topic_id, self.client_id
        ).await?;
        let server_sync_step_1 = build_server_sync_step_1(_last_received_offset, self.client_id);
        websocket_sender.send(Message::Binary(server_sync_step_1.encode_v1().into()))
            .instrument(info_span!("send_sync_step_one_ws_message"))
            .await?;
        event!(
            name: "server_sync_step_one_canonical_log_line",
            Level::INFO,
            duration_ns=start.elapsed().as_nanos(),
            topic_id=self.topic_id.as_hyphenated().to_string(),
            user_id=self.user_id.as_hyphenated().to_string(),
            client_id_src=self.client_id,
            last_received_offset=_last_received_offset,
            "server_sync_step_one_canonical_log_line",
        );
        Ok(())
    }

    fn parse_client_sync_step_one_to_pairs(
        &self,
        encoded_sv: &Vec<u8>,
    ) -> Result<Vec<(u64, u32)>, TaskError> {
        let decoded_state_vector = StateVector::decode_v1(&encoded_sv)?;
        event!(
            Level::TRACE,
            client_id_src=self.client_id,
            "client state vector: {:?}", decoded_state_vector,
        );
        let pairs = decoded_state_vector
            .iter()
            .map(|(k, v)| (*k, *v))
            .collect();
        // remember that this list of pairs is a series of exclusive upper bounds on the 
        // offsets of operations that have been received per client. Meaning that each 
        // offset in the vec of pairs is the next offset that is expected to be received,
        // not the offset of the most recent operation that has been received.
        Ok(pairs)
    }

    #[instrument(skip_all)]
    async fn process_client_sync_step_one(
        &self,
        writer: Writer<WriterAwaitingHandshake>,
        encoded_sv: Vec<u8>,
        start: Instant,
        websocket_sender: &mut SplitSink<WebSocket, Message>,
    ) -> Result<Writer<WriterHotPath>, TaskError> {
        // create a list of pairs that can be used to query the repo
        let pairs = self.parse_client_sync_step_one_to_pairs(&encoded_sv)?;
        // read all the updates from the repo with a happens after relationship with the
        // state vector
        let happens_after_updates = self.repo.read_operations_after(
            &pairs, self.topic_id,
        ).await?;
        // merge the happens after updates into one encoded bulk update
        // TODO: we should also be reading deletions from the db and merging them into
        // the bulk update message
        let (hot_path_writer, insertions, encoded_bulk_update) = {
            let mut decoded_updates = Vec::<Update>::new();
            for encoded_update in &happens_after_updates {
                decoded_updates.push(Update::decode_v1(&encoded_update)?)
            }
            let client_state_vector = StateVector::decode_v1(&encoded_sv)?;
            let (hot_path_writer, decoded_bulk_update) = writer.receive_state_vector(
                client_state_vector, decoded_updates
            );
            (hot_path_writer, decoded_bulk_update.insertions(false), decoded_bulk_update.encode_v1())
        };
        let client_state_vector = StateVector::decode_v1(&encoded_sv)?;
        event!(
            name: "writer_client_sync_step_one_canonical_log_line",
            Level::INFO,
            duration_ns=start.elapsed().as_nanos(),
            count_updates=happens_after_updates.len(),
            topic_id=self.topic_id.as_hyphenated().to_string(),
            user_id=self.user_id.as_hyphenated().to_string(),
            client_id_src=self.client_id,
            client_state_vector=?client_state_vector,
            update_insertions=?insertions,
            "received client sync step one message and constructed server sync step two, transitioning writer from handshake to hot path"
        );
        // send the bulk update over the websocket connection
        let ws_message = Message::Binary(
            SyncMessage::SyncStep2(encoded_bulk_update)
                .encode_v1()
                .into()
        );
        websocket_sender.send(ws_message)
            .instrument(info_span!("send_server_sync_step_two_ws_message"))
            .await?;
        
        Ok(hot_path_writer)
    }

    #[instrument(skip_all,fields(skipped_message))]
    async fn write_hot_path_loop(
        &self,
        update: UpdateMessage, 
        websocket_sender: &mut SplitSink<WebSocket, Message>
    ) -> Result<(), TaskError> {
        let start = Instant::now();
        let mut skipped_message = true;
        if *update.key() != self.client_id {
            skipped_message = false;
            let ws_message = Message::Binary(SyncMessage::Update(update.payload.to_vec()).encode_v1().into());
            // TODO: batch read messages from the broker queue, use try receive
            // TODO: batch send messages, concatenate many broker messages into one ws message
            let ws_send_result = websocket_sender.send(ws_message)
                .instrument(info_span!("send_update_ws_message"))
                .await;
            if let Err(e) = ws_send_result {
                tracing::Span::current().record("skipped_message", skipped_message);
                // TODO: having two invocations of the event macro is very messy, work on reducing code duplication
                event!(
                    name: "writer_hot_path_canonical_log_line",
                    Level::ERROR,
                    duration_ns=start.elapsed().as_nanos(),
                    skipped_message,
                    topic_id=self.topic_id.as_hyphenated().to_string(),
                    user_id=self.user_id.as_hyphenated().to_string(),
                    client_id_src=update.client_id,
                    client_id_dst=self.client_id,
                    offset=update.offset,
                    error=%e,
                    "writer_hot_path_canonical_log_line",
                );        
                return Err(e.into());
            }
        }
        tracing::Span::current().record("skipped_message", skipped_message);
        event!(
            name: "writer_hot_path_canonical_log_line",
            Level::INFO,
            duration_ns=start.elapsed().as_nanos(),
            skipped_message,
            topic_id=self.topic_id.as_hyphenated().to_string(),
            user_id=self.user_id.as_hyphenated().to_string(),
            client_id_src=update.client_id,
            client_id_dst=self.client_id,
            offset=update.offset,
            "writer_hot_path_canonical_log_line",
        );
        return Ok(());
    }

    pub async fn write(
        &self,
        mut websocket_sender: SplitSink<WebSocket, Message>,
        mut broker_receiver: WrappedReceiver<Uuid, UpdateMessage>,
        sync_receiver: Receiver<ReaderEvent>,
        cancel_token: CancellationToken,
    ) -> Result<(), TaskError> {
        self.send_server_sync_step_one(&mut websocket_sender).await?;

        // create an instance of the writer state
        let writer = Writer::new(
            self.topic_id, self.user_id, self.client_id
        );
        // while in the writer awaiting handshake state, we can only receive state vector messages from the 
        // client indicating the messages that the client has already received. Only then can we start 
        // sending the client update messages
        let _writer = loop {
            tokio::select! {
                biased;
                _ = cancel_token.cancelled() => {
                    // TODO: should we be sending a closing frame here?
                    return Ok(());
                },
                result = sync_receiver => match result {
                    Ok(ReaderEvent::ClientSyncStep1(encoded_sv, start)) => {
                        break self.process_client_sync_step_one(writer, encoded_sv, start, &mut websocket_sender).await?;
                    },
                    Err(_) => {
                        // the sync channel returning none means that the channel has been closed
                        // this can only happen if the read task has completed. In that case we 
                        // want to send a closing frame over the websocket connection then close 
                        // the websocket connection
                        let _ = websocket_sender.send(Message::Close(Some(
                            CloseFrame { code: 1011, reason: "internal server error".into()},
                        )));
                        return Ok(());
                    }
                }
            }
        };

        loop {
            // we need to use this tokio select statement to prevent dangling write tasks
            // in the case that the websocket disconnects, the write task may not know that 
            // the websocket connection has already dropped if there are not other clients
            // sending messages for the write task to send over the channel
            // using a oneshot channel to communicate from the reader to the writer allows
            // us to know that the connection has been dropped as soon as we have read the
            // last message
            tokio::select! {
                biased;
                _ = cancel_token.cancelled() => {
                    // TODO: should we be sending a closing frame here
                    return Ok(());
                },
                message = broker_receiver.recv() => {
                    match message {
                        Ok(msg) => {
                            self.write_hot_path_loop(msg, &mut websocket_sender).await?;
                        },
                        Err(e @ BroadcastRVError::Closed) => {
                            // handle closure of the receiver from the broker
                            // this is an unrecoverable internal server error, we should close the connection
                            event!(name:"closed_broker_receiver", Level::WARN, client_id_dst=self.client_id);
                            let _ = websocket_sender.send(Message::Close(Some(
                                CloseFrame { code: 1011, reason: "internal server error".into()},
                            )));
                            return Err(TaskError::BrokerReceiveError(e));
                        },
                        Err(BroadcastRVError::Lagged(count_missed)) => {
                            event!(
                                name: "lagged_broker_receiver", 
                                Level::WARN, 
                                count_missed,
                                client_id_dst=self.client_id,
                            );
                            // TODO: this is the case in which we missed some messages sent to this topic
                            //       by some fast senders. We should read the missed messages from the
                            //       database so that we can catch up
                        }
                    }
                },
                // we don't need to listen for more messages from rx_sync here because in order to 
                // get to the hot path of the protocol we must have already consumed one of the 
                // sync step one messages already.
            }
        }
    }
}
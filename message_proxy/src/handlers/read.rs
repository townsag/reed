use tokio::sync::broadcast::{
    Sender as BCSender, 
};
use tokio::sync::oneshot::{
    Sender
};
use yrs::updates::encoder::Encode;
use std::sync::Arc;
use std::time::Instant;
use tokio_util::sync::CancellationToken;
use axum::{
    extract::ws::{
        Message, 
        WebSocket,
    },
    Error,
};
use futures_util::{
    StreamExt, 
    stream::{SplitStream},
};
use yrs::{
    StateVector, Update,
    sync::protocol::SyncMessage,
    updates::decoder::Decode,
};
use tracing::{Level, event, instrument};

use crate::repository::{Repository};
use crate::config::otel::{
    WSMessageType,
};
use crate::handlers::{
    WebsocketHandler, 
    Decoded,
    TaskError,
    ReaderEvent,
    UpdateMessage
};

impl <R: Repository> WebsocketHandler<R> {
    #[instrument(skip_all)]
    fn forward_client_sync_step_one(
        sync_sender_opt: &mut Option<Sender<ReaderEvent>>,
        state_vector: StateVector,
        start: Instant,
    ) -> Result<(), TaskError> {
        // sync step one messages get processed by the writer
        // TODO: may need some error handling code here in the case that 
        // the client receiver has already been dropped
        if let Some(sync_sender) = sync_sender_opt.take() {
            sync_sender.send(ReaderEvent::ClientSyncStep1(state_vector.encode_v1(), start))
                .map_err(TaskError::ReaderToWriterSendError)?;
        }
        Ok(())
    }

    async fn decode_helper(
        &self,
        result: Option<Result<Message, Error>>,
        client_id: u64,
    ) -> Decoded {
        /*
        This result can be one of these things:
        - Binary websocket message
            - when we get these we want to decode them into one of a few types of Sync Messages
            - alternatively, if we fail to decode we want to close the connection 
        - Closing frame websocket message
            - when we get these we want to indicate to the writer that the websocket connection is closing
        - Ping, Pong, Text websocket message
            - we are not interested in these
        - Err
            - these indicate that there was an error buffering messages at the OS level
            - when we get these we want to indicate to the writer that the websocket connection is closing
        - None
            - these indicate that the websocket connection has closed without a closing frame
            - when we get these we want to indicate to the writer that the websocket connection is closing
        */
        let start = Instant::now();
        match result {
            Some(Ok(Message::Binary(bytes))) => {
                match SyncMessage::decode_v1(&bytes) {
                    Ok(sync_message) => {
                        let message_type = match sync_message {
                            SyncMessage::SyncStep1(_) => { WSMessageType::SyncStep1 },
                            SyncMessage::SyncStep2(_) => {WSMessageType::SyncStep2 },
                            SyncMessage::Update(_) => { WSMessageType::Update },
                        };
                        self.metrics_ws.record_received_ws_message(bytes.len(), message_type);
                        Decoded::Valid(sync_message, start)
                    },
                    Err(e) => {
                        self.metrics_ws.record_received_ws_message(
                            bytes.len(), WSMessageType::Error
                        );
                        event!(Level::WARN, client_id_src=client_id, "failed to decode message with error: {e}");
                        Decoded::Skip
                    },
                }
            },
            Some(Ok(Message::Close(_))) | None | Some(Err(_)) => {
                Decoded::Failure
            },
            Some(Ok(_)) => Decoded::Skip,
        }
    }

    fn parse_new_offset(
        &self,
        encoded_update: &Vec<u8>,
    ) -> Result<Option<u32>, TaskError> {
        let decoded_update = Update::decode_v1(&encoded_update)?;
        event!(
            Level::DEBUG,
            client_id_src=self.client_id,
            insertions=?&decoded_update.insertions(false),
            "received update information"
        );
        // find the inclusive upper bound of operation offsets contained by this update
        // for the src client id associated with this connection
        let insertions = decoded_update.insertions(false);
        let new_offset: Option<u32> = match insertions.get(&self.client_id) {
            Some(id_range) => {
                // this gives us an iterator over the ranges of operations in the id range
                // this is because the id range can be disjoint instead of continuous
                let new_offset = id_range.iter()
                    // this gives us the last range in the iterator of ranges
                    .last()
                    // this gives us the exclusive upper bound of the last range 
                    .map(|r| r.end)
                    // this gives us the inclusive upper bound of the last range
                    .map(|o| o - 1);
                new_offset
            },
            // this is the case where there are no operations in the update for the current client_id
            None => None
        };
        Ok(new_offset)
    }

    #[instrument(skip_all)]
    async fn process_client_sync_step_two(
        &self,
        encoded_update: Vec<u8>,
        broker_sender: &BCSender<UpdateMessage>,
        start: Instant,
    ) -> Result<Option<u32>, TaskError> {
        // if it is a sync step two message, process it at the reader then break
        /*
        - Yrs has zero indexed updates
        - However if no operations are found for a client_id in a state_vector the
        default value is zero
            - this is like saying that clients who have made no updates have made one update
        
        ^This is only partially true. Updates are zero indexed, however the state vector
        holds the exclusive upper bound of operations received. Meaning that the state vector
        stores the offset of the next operation a client expects to receive
        
        - when we receive a client sync step two message, we need to throw it out 
        if it has no operations in it because we don't want to save an empty update
        at a offset that a valid update would be saved at
        */
        // decoded Updates cannot be held across await boundaries because it is not send
        // when possible, we need to drop the update before an await boundary
        let new_offset = self.parse_new_offset(&encoded_update)?;
        // if the message had new operations in it, write the message to the database
        let mut skipped_persistence = true;
        let mut update_size_bytes = 0;
        if let Some(new_offset) = new_offset {
            let last_received_offset = self.repo.read_last_received_offset(
                self.topic_id,
                self.client_id,
            ).await?;
            // drop the update if the offset of the update is less than or equal 
            // to the offset of the most recent update from this client
            if last_received_offset.is_none_or(|x| x < new_offset) {
                skipped_persistence = false;
                update_size_bytes = encoded_update.len();

                self.repo.write_operation(
                    self.topic_id, self.user_id, self.client_id, 
                    new_offset, &encoded_update
                ).await?;
                // write the bulk update to the broadcast channel
                // only broadcast updates that are not duplicates
                let update_message = UpdateMessage{
                    client_id: self.client_id, 
                    payload: Arc::new(encoded_update), 
                    offset: new_offset,
                };
                broker_sender.send(update_message)?;
            } else {
                event!(
                    Level::DEBUG,
                    client_id_src=self.client_id,
                    "skipping persisting this operation because the old offset ({:?}) and new offset ({}) are >=",
                    last_received_offset,
                    new_offset,
                )
            }
        }
        event!(
            name: "reader_client_sync_step_two_canonical_log_line",
            Level::INFO,
            last_offset_from_client=new_offset,
            duration_ns=start.elapsed().as_nanos(),
            skipped_persistence,
            update_size_bytes,
            topic_id=self.topic_id.as_hyphenated().to_string(),
            user_id=self.user_id.as_hyphenated().to_string(),
            client_id_src=self.client_id,
            "received an persisted client sync step two message, transitioning from handshake to hot path for reader",
        );
        return Ok(new_offset);
    }

    #[instrument(skip_all)]
    async fn reader_hot_path(
        &self,
        encoded_update: Vec<u8>,
        sender_broker: &BCSender<UpdateMessage>,
        start: Instant,
    ) -> Result<Option<u32>, TaskError> {
        let update_size_bytes = encoded_update.len();
        // if it is an update message, process it with the reader then forward
        // it to other connected clients using the broker
        let new_offset = self.parse_new_offset(&encoded_update)?;
        if let Some(offset) = new_offset {
            self.repo.write_operation(
                self.topic_id, self.user_id, self.client_id, 
                offset, &encoded_update,
            ).await?;
            let message = UpdateMessage { 
                client_id: self.client_id, 
                payload: Arc::new(encoded_update),
                offset: offset,
            };
            sender_broker.send(message)?;
        }
        event!(
            name: "reader_hot_path_canonical_log_line",
            Level::INFO,
            new_offset,
            skipped=new_offset.is_none(),
            update_size_bytes,
            duration_ns = start.elapsed().as_nanos(),
            topic_id=self.topic_id.as_hyphenated().to_string(),
            user_id=self.user_id.as_hyphenated().to_string(),
            client_id_src=self.client_id,
            "reader_hot_path_canonical_log_line",
        );
        return Ok(new_offset);
    }


    pub async fn read(
        &self,
        mut websocket_receiver: SplitStream<WebSocket>,
        broker_sender: BCSender<UpdateMessage>,
        sync_sender: Sender<ReaderEvent>,
        cancel_token: CancellationToken,
    ) -> Result<(), TaskError> {
        let mut sync_sender_opt = Some(sync_sender);
        // expect the client to send us a sync step one message including their version vector
        // or a sync step two message including the local updates that the server does not yet have
        let mut _last_received_offset = loop {
            tokio::select! {
                biased;
                _ = cancel_token.cancelled() => {
                    return Ok(());
                },
                result = websocket_receiver.next() => match self.decode_helper(result, self.client_id).await {
                    Decoded::Valid(SyncMessage::SyncStep1(sv), start) => {
                        let _ = Self::forward_client_sync_step_one(&mut sync_sender_opt, sv, start);
                    },
                    Decoded::Valid(SyncMessage::SyncStep2(encoded_bulk_update), start) => {
                        break self.process_client_sync_step_two(
                            encoded_bulk_update,
                            &broker_sender,
                            start,
                        ).await?;
                    },
                    // if it is an update message, skip it
                    Decoded::Valid(SyncMessage::Update(_), _) => {},
                    Decoded::Skip => {},
                    Decoded::Failure => { 
                        return Ok(()); 
                    }
                },
            }
        };
        event!(Level::DEBUG, client_id_src=self.client_id, "entering reader hot path");
        // enter the hot path
        loop {
            tokio::select! {
                biased;
                _ = cancel_token.cancelled() => {
                    event!(Level::DEBUG, client_id_src=self.client_id, "returning from reader because of cancellation token");
                    return Ok(());
                },
                result = websocket_receiver.next() => match self.decode_helper(result, self.client_id).await {
                    Decoded::Valid(SyncMessage::SyncStep1(sv), start) => {
                        let _ = Self::forward_client_sync_step_one(&mut sync_sender_opt, sv, start);
                    },
                    Decoded::Valid(SyncMessage::SyncStep2(_), _) => { 
                        /* if it is a sync step two message, ignore it. 
                        We must have already processed the client sync 
                        step two message in order to get to the reader hot path */ 
                    },
                    // if it is an update message, skip it
                    Decoded::Valid(SyncMessage::Update(encoded_update), start) => {
                        _last_received_offset = self.reader_hot_path(
                            encoded_update, &broker_sender, start,
                        ).await?
                    },
                    Decoded::Skip => {},
                    Decoded::Failure => { return Ok(()); }
                },
            }
        }
    }
}
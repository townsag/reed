// use std::error::Error;
// option tab is the command to prompt VSCode to suggest symbols
use tokio::sync::broadcast::{
    Sender as BCSender, 
    error::RecvError as BroadcastRVError,
};
use tokio::sync::oneshot::{
    self, Receiver, Sender
    // error::RecvError,
};
use yrs::updates::encoder::Encode;
use std::sync::Arc;
use std::time::Instant;
use tokio::task::JoinSet;
use tokio_util::sync::CancellationToken;
use uuid::Uuid;
use axum::{
    extract::ws::{
        Message, 
        WebSocket, 
        WebSocketUpgrade, 
        CloseFrame,
    },
    extract::Path,
    extract::Query,
    extract::State,
    response::Response,
    Error,
};
use futures_util::{
    SinkExt, 
    StreamExt, 
    stream::{SplitStream, SplitSink},
};
use yrs::{
    StateVector, Update,
    sync::protocol::SyncMessage,
    updates::decoder::Decode, 
};
use crate::broker::{Routable, WrappedReceiver};
use crate::repository::{Repository, RepoError};
use crate::AppState;
use crate::state_machine::{Writer, WriterAwaitingHandshake, WriterHotPath};
use thiserror::Error;
use serde;
use tracing::{event, Level};

#[derive(Debug)]
enum ReaderEvent {
    // ClosedByClient,
    // ClosedByServer,
    ClientSyncStep1(Vec<u8>),
}

#[derive(Clone,Debug)]
pub struct UpdateMessage {
    client_id: u64,
    payload: Arc<Vec<u8>>,
}

impl Routable for UpdateMessage {
    type Key = u64;
    fn key(&self) -> &Self::Key {
        return &self.client_id;
    }
}

#[derive(serde::Deserialize)]
pub struct ClientParams {
    pub client_id: u64,
}

// #[axum::debug_handler]
pub async fn handler<R: Repository>(
    ws: WebSocketUpgrade, 
    Path((topic_id, user_id)): Path<(Uuid, Uuid)>,
    Query(client_params): Query<ClientParams>,
    State(state): State<AppState<R>>,
) -> Response {
    ws.on_upgrade(move |socket| handle_socket(socket, topic_id, user_id, client_params.client_id, state))
}

enum Decoded {
    // TODO: if we find that the cost of encoding and decoding is too expensive we should find
    // an alternative pattern
    Valid(SyncMessage),
    Skip,
    Failure,
}

async fn decode_helper(
    result: Option<Result<Message, Error>>, 
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
    event!(Level::INFO, "received ws message: {:?}", result);
    match result {
        Some(Ok(Message::Binary(bytes))) => {
            event!(Level::INFO, "decoding message");
            match SyncMessage::decode_v1(&bytes) {
                Ok(sync_message) => {
                    event!(Level::INFO, "decoded valid sync message: {:?}", sync_message);
                    Decoded::Valid(sync_message)
                },
                Err(e) => {
                    event!(Level::INFO, "failed to decode message with error: {e}");
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
    event!(Level::INFO, "read last received offset: {:?}", last_received_offset);
    /*
    State vectors in Yrs are zero indexed, meaning that a state vector that has received no operations 
    from a client has no record of that client (instead of having the key value pair client_id: 0).
    For this reason we have to send an empty version vector if we have seen no operations from this client
     */
    // send a message to the client indicating what updates we have already so that the client can send us
    // a bulk update of messages that we are missing 
    let mut reader_sv = StateVector::default();
    if let Some(op) = last_received_offset {
        reader_sv.set_max(client_id, op);
    }
    event!(Level::INFO, "sending state vector from writer to reader: {:?}", reader_sv);
    SyncMessage::SyncStep1(reader_sv)
}

struct WebsocketHandler<R: Repository> {
    topic_id: Uuid,
    user_id: Uuid,
    client_id: u64,
    repo: R,
}

impl <R: Repository> WebsocketHandler<R> {
    fn new(
        topic_id: Uuid,
        user_id: Uuid,
        client_id: u64,
        repo: R,
    ) -> Self {
        WebsocketHandler { topic_id, user_id, client_id, repo }
    }

    fn forward_client_sync_step_one(
        sync_sender_opt: &mut Option<Sender<ReaderEvent>>,
        state_vector: StateVector,
    ) -> Result<(), TaskError>{
        // sync step one messages get processed by the writer
        // TODO: may need some error handling code here in the case that 
        // the client receiver has already been dropped
        if let Some(sync_sender) = sync_sender_opt.take() {
            sync_sender.send(ReaderEvent::ClientSyncStep1(state_vector.encode_v1()))
                .map_err(TaskError::ReaderToWriterSendError)?;
        }
        Ok(())
    }

    async fn process_client_sync_step_two(
        &self,
        encoded_update: Vec<u8>,
        broker_sender: &BCSender<UpdateMessage>
    ) -> Result<Option<u32>, TaskError> {
        // if it is a sync step two message, process it at the reader then break
        /*
        - Yrs has zero indexed updates
        - However if no operations are found for a client_id in a state_vector the
        default value is zero
            - this is like saying that clients who have made no updates have made one update
        
        - when we receive a client sync step two message, we need to throw it out 
        if it has no operations in it because we don't want to save an empty update
        at a offset that a valid update would be saved at
        */
        let new_offset = {
            let client_bulk_update = Update::decode_v1(&encoded_update)?;
            event!(Level::INFO, "received client sync step two message {client_bulk_update}");
            let update_state_vector = client_bulk_update.state_vector_lower();
            if !update_state_vector.contains_client(&self.client_id) {
                event!(Level::INFO, "skipping writing this update to the db because it contains no operations");
                return Ok(None);
            }
            update_state_vector.get(&self.client_id)
        };
        // decoded Updates cannot be held across await boundaries because it is not send
        // when possible, we need to drop the update before an await boundary
        self.repo.write_operation(
            self.topic_id, self.user_id, self.client_id, 
            new_offset, &encoded_update
        ).await?;
        event!(Level::INFO, "finished writing client sync step two operation");
        // write the bulk update to the broadcast channel
        let update_message = UpdateMessage{
            client_id: self.client_id, payload: Arc::new(encoded_update),
        };
        broker_sender.send(update_message)?;
        return Ok(Some(new_offset));
    }

    async fn reader_hot_path(
        &self,
        encoded_update: Vec<u8>,
        sender_broker: &BCSender<UpdateMessage>,
    ) -> Result<Option<u32>, TaskError> {
        let start = Instant::now();
        let update_size_bytes = encoded_update.len();
        // if it is an update message, process it with the reader then forward
        // it to other connected clients using the broker
        let new_offset = {
            let update = Update::decode_v1(&encoded_update)?;
            event!(
                Level::INFO,
                "received update message {:?} with offset {}",
                update, update.state_vector_lower().get(&self.client_id),
            );
            event!(Level::INFO, "with state vector: {:?}", update.state_vector_lower());
            update.state_vector_lower().get(&self.client_id)
        };
        self.repo.write_operation(
            self.topic_id, self.user_id, self.client_id, 
            new_offset, &encoded_update,
        ).await?;
        let message = UpdateMessage { 
            client_id: self.client_id, 
            payload: Arc::new(encoded_update),
        };
        sender_broker.send(message)?;
        event!(
            name: "reader_hot_path_canonical_log_line",
            Level::INFO,
            new_offset,
            update_size_bytes,
            duration = start.elapsed().as_millis(),
            "completed reader hot path loop",
        );
        return Ok(Some(new_offset));
    }

    async fn read(
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
                result = websocket_receiver.next() => match decode_helper(result).await {
                    Decoded::Valid(SyncMessage::SyncStep1(sv)) => {
                        let _ = Self::forward_client_sync_step_one(&mut sync_sender_opt, sv);
                    },
                    Decoded::Valid(SyncMessage::SyncStep2(encoded_bulk_update)) => {
                        break self.process_client_sync_step_two(
                            encoded_bulk_update,
                            &broker_sender,
                        ).await?;
                    },
                    // if it is an update message, skip it
                    Decoded::Valid(SyncMessage::Update(_)) => {},
                    Decoded::Skip => {},
                    Decoded::Failure => { 
                        return Ok(()); 
                    }
                },
            }
        };
        event!(Level::INFO, "entering reader hot path");
        // enter the hot path
        loop {
            tokio::select! {
                biased;
                _ = cancel_token.cancelled() => {
                    event!(Level::INFO, "returning from reader because of cancellation token");
                    return Ok(());
                },
                result = websocket_receiver.next() => match decode_helper(result).await {
                    Decoded::Valid(SyncMessage::SyncStep1(sv)) => {
                        let _ = Self::forward_client_sync_step_one(&mut sync_sender_opt, sv);
                    },
                    Decoded::Valid(SyncMessage::SyncStep2(_)) => { 
                        /* if it is a sync step two message, ignore it. 
                        We must have already processed the client sync 
                        step two message in order to get to the reader hot path */ 
                    },
                    // if it is an update message, skip it
                    Decoded::Valid(SyncMessage::Update(encoded_update)) => {
                        _last_received_offset = self.reader_hot_path(
                            encoded_update, &broker_sender
                        ).await?
                    },
                    Decoded::Skip => {},
                    Decoded::Failure => { return Ok(()); }
                },
            }
        }
    }
    /*
    CHECKPOINT:
    - you were in the middle of decomposing the reader and writer tasks into methods on the WebsocketHandler struct
    - decompose the write task into methods on the WebsocketHandler
    - The reason that you are making this change is because separating the handling of each websocket message type 
      into multiple functions allows us to easily instrument each function with async tracing instrumentation. Furthermore,
      as the logging boilerplate for each operation type proliferates, it will be nice for them to be decomposed so
      it's not like looking at one unmanageable wall of text
     */

    async fn process_client_sync_step_one(
        &self,
        writer: Writer<WriterAwaitingHandshake>,
        encoded_sv: Vec<u8>,
    ) -> Result<(Writer<WriterHotPath>, Vec<u8>), TaskError> {
        let pairs = {
        let client_state_vector = StateVector::decode_v1(&encoded_sv)?;
            writer.prepare_sync_step_2(client_state_vector)
        };
        let happens_after_updates = self.repo.read_operations_after(
            &pairs, self.topic_id,
        ).await?;
        let mut decoded_updates = Vec::<Update>::new();
        for encoded_update in happens_after_updates {
            decoded_updates.push(Update::decode_v1(&encoded_update)?)
        }
        let client_state_vector = StateVector::decode_v1(&encoded_sv)?;
        let (hot_path_writer, decoded_bulk_update) = writer.receive_state_vector(
            client_state_vector, decoded_updates
        );
        
        Ok((hot_path_writer, decoded_bulk_update.encode_v1()))
    }

    async fn write_hot_path_loop(
        &self,
        update: UpdateMessage, 
        websocket_sender: &mut SplitSink<WebSocket, Message>
    ) -> Result<(), TaskError> {
        if *update.key() == self.client_id {
            event!(Level::INFO, "skipping message: {:?}", update);
            // TODO: how come hyphenated does not consume self?
            // TODO: check that this does what I think it does, can continue be used
            //       inside of a tokio select statement
            return Ok(());
        }
        let ws_message = Message::Binary(SyncMessage::Update(update.payload.to_vec()).encode_v1().into());
        // TODO: batch read messages from the broker queue, use try receive
        // TODO: batch send messages, concatenate many broker messages into one ws message
        websocket_sender.send(ws_message).await?;
        return Ok(());
    }

    async fn write(
        &self,
        mut websocket_sender: SplitSink<WebSocket, Message>,
        mut broker_receiver: WrappedReceiver<Uuid, UpdateMessage>,
        sync_receiver: Receiver<ReaderEvent>,
        cancel_token: CancellationToken,
    ) -> Result<(), TaskError> {
        let mut _last_received_offset = self.repo.read_last_received_offset(self.client_id).await?;
        let server_sync_step_1 = build_server_sync_step_1(_last_received_offset, self.client_id);
        websocket_sender.send(Message::Binary(server_sync_step_1.encode_v1().into())).await?;

        // create an instance of the writer state
        let writer = Writer::new(
            self.topic_id, self.user_id, self.client_id
        );
        // while in the writer awaiting handshake state, we can only receive state vector messages from the 
        // client indicating the messages that the client has already received. Only then can we start 
        // sending the client update messages
        let (_writer , bulk_update) = loop {
            tokio::select! {
                biased;
                _ = cancel_token.cancelled() => {
                    // TODO: should we be sending a closing frame here?
                    return Ok(());
                },
                result = sync_receiver => match result {
                    Ok(ReaderEvent::ClientSyncStep1(encoded_sv)) => {
                        break self.process_client_sync_step_one(writer, encoded_sv).await?;
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
        // send the bulk update over the websocket connection
        let ws_message = Message::Binary(SyncMessage::SyncStep2(bulk_update).encode_v1().into());
        websocket_sender.send(ws_message).await?;

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
                            let _ = websocket_sender.send(Message::Close(Some(
                                CloseFrame { code: 1011, reason: "internal server error".into()},
                            )));
                            return Err(TaskError::BrokerReceiveError(e));
                        },
                        Err(BroadcastRVError::Lagged(_)) => {
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

#[derive(Error, Debug)]
enum TaskError {
    #[error("failed to decode")]
    DecodeError(#[from] yrs::encoding::read::Error),
    // TODO: make this more precise
    #[error("failed to perform repository operation")]
    PersistenceError(#[from] RepoError),
    #[error("failed to send ws message")]
    WSWriteMessage(#[from] axum::Error),
    #[error("failed to receive from the broker")]
    BrokerReceiveError(#[from] tokio::sync::broadcast::error::RecvError),
    #[error("failed to send a message from the reader to the writer")]
    ReaderToWriterSendError(ReaderEvent),
    #[error("failed to send an update message from the reader to the broker")]
    ReaderToBroadcastSendError(#[from] tokio::sync::broadcast::error::SendError<UpdateMessage>),
}

// TODO: parse the user id from a query parameter with a jwt in it
async fn handle_socket<R: Repository>(
    socket: WebSocket,
    topic_id: Uuid,
    user_id: Uuid,
    client_id: u64,
    state: AppState<R>,
) {
    // register the websocket connection with the broker
    let (
        broker_sender, broker_receiver
    ) = state.broker.register(topic_id);
    // create a oneshot channel that the read task can use to send websocket lifecycle
    // events to the write task
    let (
        sync_sender, sync_receiver
    ) = oneshot::channel();
    // create a cancellation token that write task can use to signal to the read task that 
    // it should shut down. We use a cancellation token here instead of a channel because 
    // we do not need to communicate the reason for cancellation
    let cancel_token = CancellationToken::new();
    event!(Level::INFO, "processing connection for: topic_id: {topic_id},  user_id: {user_id}, client_id: {client_id}");
    // split the websocket into a message sender and a message receiver task
    // we will use the receiver to send messages from the client to the broker and the sender 
    // to send messages from the broker to the client
    let (websocket_sender, websocket_receiver) = socket.split();
    // we spawn two threads then pass the references to websocket handler into each thread
    // you and I know that the threads will both exit before the handle_socket function
    // exits so there is no chance that the websocket handler (stack data) will go out 
    // of scope before the read or write tasks end. However in oder to convince the compiler
    // of that, we need to wrap the WebsocketHandler in an Arc so that it has 'static
    let websocket_handler = Arc::new(WebsocketHandler::new(
        topic_id, user_id, client_id, state.repo,
    ));
    let handler_read = Arc::clone(&websocket_handler);
    let handler_write = Arc::clone(&websocket_handler);
    let cancel_token_read = cancel_token.clone();
    let cancel_token_write = cancel_token.clone();

    let mut set = JoinSet::new();

    set.spawn(async move { handler_read.read(websocket_receiver, broker_sender, sync_sender, cancel_token_read).await });
    set.spawn(async move { handler_write.write(websocket_sender, broker_receiver, sync_receiver, cancel_token_write).await });
    while let Some(_) = set.join_next().await {
        // TODO: this is messy, we are not yet sure which task is returning this error
        // so we could be calling cancel on the token after the read task has already returned 
        cancel_token.cancel();
    }

    // TODO: we might want to do some book-keeping when the websocket connection closes
    //       return some sort of error value from the read and write tasks and use that 
    //       for book keeping
}

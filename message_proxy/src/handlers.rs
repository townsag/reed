// use std::error::Error;
// option tab is the command to prompt VSCode to suggest symbols
use tokio::sync::broadcast::{
    Sender as BCSender, 
    error::RecvError,
};
use tokio::sync::mpsc::{
    self, Receiver as MPSCReceiver, Sender as MPSCSender
};
use yrs::updates::encoder::Encode;
use std::sync::Arc;
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
use crate::state_machine::{Writer};
use thiserror::Error;
use serde;
use tracing::{event, Level};

enum ReaderEvent {
    // ClosedByClient,
    // ClosedByServer,
    ServerSyncStep1(Vec<u8>),
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

#[derive(Clone,Debug)]
struct SessionContext {
    topic_id: Uuid,
    user_id: Uuid,
    client_id: u64,
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

// read messages from the websocket connection and send those messages to the broker
async fn read<R: Repository>(
    mut receiver: SplitStream<WebSocket>, 
    sender_broker: BCSender<UpdateMessage>,
    tx_sync: MPSCSender<ReaderEvent>,
    repo: R,
    session_context: SessionContext,
    cancel_read_token: CancellationToken,
) -> Result<(), TaskError> {
    let mut _last_received_offset = repo.read_last_received_offset(session_context.client_id).await?;
    event!(Level::INFO, "read last received offset: {:?}", _last_received_offset);
    /*
    State vectors in Yrs are zero indexed, meaning that a state vector that has received no operations 
    from a client has no record of that client (instead of having the key value pair client_id: 0).
    For this reason we have to send an empty version vector if we have seen no operations from this client
     */
    // send a message to the client indicating what updates we have already so that the client can send us
    // a bulk update of messages that we are missing 
    let mut reader_sv = StateVector::default();
    if let Some(op) = _last_received_offset {
        reader_sv.set_max(session_context.client_id, op);
    }
    event!(Level::INFO, "sending state vector from writer to reader: {:?}", reader_sv);
    let encoded_state_vector = reader_sv.encode_v1();
    tx_sync.send(ReaderEvent::ServerSyncStep1(encoded_state_vector)).await?;

    // expect the client to send us a sync step one message including their version vector
    // or a sync step two message including the local updates that the server does not yet have
    _last_received_offset = loop {
        tokio::select! {
            biased;
            _ = cancel_read_token.cancelled() => {
                return Ok(());
            },
            result = receiver.next() => match decode_helper(result).await {
                Decoded::Valid(SyncMessage::SyncStep1(sv)) => {
                    // sync step one messages get processed by the writer
                    // TODO: may need some error handling code here in the case that 
                    // the client receiver has already been dropped
                    tx_sync.send(ReaderEvent::ClientSyncStep1(sv.encode_v1())).await?;
                },
                Decoded::Valid(SyncMessage::SyncStep2(encoded_bulk_update)) => {
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
                        let client_bulk_update = Update::decode_v1(&encoded_bulk_update)?;
                        event!(Level::INFO, "received client sync step two message {client_bulk_update}");
                        let update_state_vector = client_bulk_update.state_vector_lower();
                        if !update_state_vector.contains_client(&session_context.client_id) {
                            event!(Level::INFO, "skipping writing this update to the db because it contains no operations");
                            break None;
                        }
                        update_state_vector.get(&session_context.client_id)
                    };
                    // decoded Updates cannot be held across await boundaries because it is not send
                    // when possible, we need to drop the update before an await boundary
                    repo.write_operation(
                        session_context.topic_id, session_context.user_id, session_context.client_id, 
                        new_offset, &encoded_bulk_update
                    ).await?;
                    event!(Level::INFO, "finished writing client sync step two operation");
                    // write the bulk update to the broadcast channel
                    let update_message = UpdateMessage{
                        client_id: session_context.client_id, payload: Arc::new(encoded_bulk_update),
                    };
                    sender_broker.send(update_message)?;
                    break Some(new_offset)
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
            _ = cancel_read_token.cancelled() => {
                event!(Level::INFO, "returning from reader because of cancellation token");
                return Ok(());
            },
            result = receiver.next() => match decode_helper(result).await {
                Decoded::Valid(SyncMessage::SyncStep1(sv)) => {
                    // sync step one messages get processed by the writer
                    // TODO: may need some error handling code here in the case that 
                    // the client receiver has already been dropped
                    tx_sync.send(ReaderEvent::ClientSyncStep1(sv.encode_v1())).await?;
                },
                Decoded::Valid(SyncMessage::SyncStep2(_)) => { /* if it is a sync step two message, ignore it */ },
                // if it is an update message, skip it
                Decoded::Valid(SyncMessage::Update(encoded_update)) => {
                    // if it is an update message, process it with the reader then forward
                    // it to other connected clients using the broker
                    let new_offset = {
                        let update = Update::decode_v1(&encoded_update)?;
                        event!(
                            Level::INFO,
                            "received update message {:?} with offset {}",
                            update, update.state_vector_lower().get(&session_context.client_id),
                        );
                        event!(Level::INFO, "with state vector: {:?}", update.state_vector_lower());
                        update.state_vector_lower().get(&session_context.client_id)
                    };
                    repo.write_operation(
                        session_context.topic_id, session_context.user_id, session_context.client_id, 
                        new_offset, &encoded_update,
                    ).await?;
                    _last_received_offset = Some(new_offset);
                    let message = UpdateMessage { client_id: session_context.client_id, payload: Arc::new(encoded_update) };
                    sender_broker.send(message)?;
                },
                Decoded::Skip => {},
                Decoded::Failure => { return Ok(()); }
            },
        }
    }
}

#[derive(Error, Debug)]
pub enum TaskError {
    #[error("failed to decode")]
    DecodeError(#[from] yrs::encoding::read::Error),
    // TODO: make this more precise
    #[error("failed to perform repository operation")]
    PersistenceError(#[from] RepoError),
    #[error("failed to send ws message")]
    WSWriteMessage(#[from] axum::Error),
    #[error("failed to receive from the broker")]
    BrokerReceiveError(#[from] tokio::sync::broadcast::error::RecvError),
    #[error("failed to send a message from the reader to the write")]
    ReaderToWriterSendError(#[from] tokio::sync::mpsc::error::SendError<ReaderEvent>),
    #[error("failed to send an update message from the reader to the broker")]
    ReaderToBroadcastSendError(#[from] tokio::sync::broadcast::error::SendError<UpdateMessage>),
}

// receive messages from the broker and send them to the websocket connection
async fn write<R: Repository>(
    mut sender: SplitSink<WebSocket, Message>, 
    mut receiver_broker: WrappedReceiver<Uuid, UpdateMessage>,
    mut rx_sync: MPSCReceiver<ReaderEvent>,
    repo: R,
    session_context: SessionContext,
    cancel_token: CancellationToken,
) -> Result<(), TaskError> {
    // create an instance of the writer state
    let writer = Writer::new(
        session_context.topic_id, session_context.user_id, session_context.client_id
    );
    // while in the writer awaiting handshake state, we can only receive state vector messages from the 
    // client indicating the messages that the client has already received. Only then can we start 
    // sending the client update messages
    let (writer , bulk_update) = loop {
        tokio::select! {
            biased;
            _ = cancel_token.cancelled() => {
                return Ok(());
            },
            result = rx_sync.recv() => match result {
                Some(ReaderEvent::ClientSyncStep1(encoded_sv)) => {
                    let pairs = {
                        let client_state_vector = StateVector::decode_v1(&encoded_sv)?;
                        writer.prepare_sync_step_2(client_state_vector)
                    };
                    let happens_after_updates = repo.read_operations_after(
                        &pairs, session_context.topic_id,
                    ).await?;
                    let mut decoded_updates = Vec::<Update>::new();
                    for encoded_update in happens_after_updates {
                        decoded_updates.push(Update::decode_v1(&encoded_update)?)
                    }
                    let client_state_vector = StateVector::decode_v1(&encoded_sv)?;
                    let (hot_path_writer, decoded_bulk_update) = writer.receive_state_vector(
                        client_state_vector, decoded_updates
                    );
                    break (hot_path_writer, decoded_bulk_update.encode_v1())
                },
                Some(ReaderEvent::ServerSyncStep1(encoded_sv)) => {
                    let decoded_sv = StateVector::decode_v1(&encoded_sv)?;
                    sender.send(
                        Message::Binary(SyncMessage::SyncStep1(decoded_sv).encode_v1().into())
                    ).await?;
                },
                None => {
                    // the sync channel returning none means that the channel has been closed
                    // this can only happen if the read task has completed. In that case we 
                    // want to send a closing frame over the websocket connection then close 
                    // the websocket connection
                    let _ = sender.send(Message::Close(Some(
                        CloseFrame { code: 1011, reason: "internal server error".into()},
                    )));
                    return Ok(());
                }
            }
        }
    };
    // send the bulk update over the websocket connection
    let ws_message = Message::Binary(SyncMessage::SyncStep2(bulk_update).encode_v1().into());
    sender.send(ws_message).await?;

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
                return Ok(());
            },
            message = receiver_broker.recv() => {
                match message {
                    Ok(msg) => {
                        if *msg.key() == session_context.client_id {
                            event!(Level::INFO, "skipping message: {:?}", msg);
                            // TODO: how come hyphenated does not consume self?
                            // TODO: check that this does what I think it does, can continue be used
                            //       inside of a tokio select statement
                            continue
                        }
                        let ws_message = Message::Binary(SyncMessage::Update(msg.payload.to_vec()).encode_v1().into());
                        // TODO: batch read messages from the broker queue, use try receive
                        // TODO: batch send messages, concatenate many broker messages into one ws message
                        sender.send(ws_message).await?;
                    },
                    Err(e @ RecvError::Closed) => {
                        // handle closure of the receiver from the broker
                        // this is an unrecoverable internal server error, we should close the connection
                        let _ = sender.send(Message::Close(Some(
                            CloseFrame { code: 1011, reason: "internal server error".into()},
                        )));
                        return Err(TaskError::BrokerReceiveError(e));
                    },
                    Err(RecvError::Lagged(_)) => {
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

// TODO: parse a 
async fn handle_socket<R: Repository>(
    socket: WebSocket,
    topic_id: Uuid,
    user_id: Uuid,
    client_id: u64,
    state: AppState<R>,
) {
    // register the websocket connection with the broker
    let (
        sender_broker, receiver_broker
    ) = state.broker.register(topic_id);
    // create a oneshot channel that the read task can use to send websocket lifecycle
    // events to the write task
    let (
        tx_sync, rx_sync
    ) = mpsc::channel(5);
    // create a cancellation token that write task can use to signal to the read task that 
    // it should shut down. We use a cancellation token here instead of a channel because 
    // we do not need to communicate the reason for cancellation
    let cancel_read_token = CancellationToken::new();
    let session_context = SessionContext{ topic_id, user_id, client_id };
    event!(Level::INFO, "processing connection for: {:?}", session_context);
    // split the websocket into a message sender and a message receiver task
    // we will use the receiver to send messages from the client to the broker and the sender 
    // to send messages from the broker to the client
    let (sender_ws, receiver_ws) = socket.split();
    let mut set = JoinSet::new();
    // TODO: it would be interesting to see if the compiler will factor the user_id.to_string() call into one call
    //       that is assigned to a variable and then passed to each of the read and write task
    // TODO: refactor the read function to return a Result type instead of nothing, this will simplify
    // much of the error handling inside of the read function
    set.spawn(read(
        receiver_ws, sender_broker, tx_sync, state.repo.clone(), session_context.clone(), cancel_read_token.clone(),
    ));
    set.spawn(write(
        sender_ws, receiver_broker, rx_sync, state.repo, session_context, cancel_read_token.clone(),
    ));
    while let Some(_) = set.join_next().await {
        // TODO: this is messy, we are not yet sure which task is returning this error
        // so we could be calling cancel on the token after the read task has already returned 
        cancel_read_token.cancel();
    }

    // TODO: we might want to do some book-keeping when the websocket connection closes
    //       return some sort of error value from the read and write tasks and use that 
    //       for book keeping
}


/*
Refactors:
- update the implementation so that the channel between the reader and the writer has just
  one purpose instead of two purposes 
- pull the triggering of the cancellation read token out of the writer and into handle socket
    - write should return an error and handle socket should cancel the read task depending on
      the error

Technical Requirement:
- we need to indicate to the client that the server will be closing the connection
- if the read task fails, we want the write task to stop too
    - there should be a message indicating that the connection is being closed by the server
- if the write task fails, we want the read task to close too
    - there will not be a message indicating that the server is closing the connection 
      because the write task has failed

- approach:
    - refactor the read task so that connection lifecycle information is no longer 
      included in the events that can be sent over the channel 
        - the channel should only be for sync step one messages
    - refactor the write task to get connection lifecycle information from the 
      cancellation token instead of the channel
        - the write task should take the token as a parameter

- How come I dont have to await sending to a broadcast channel:
    - is it because the broadcast channel has no back pressure mechanism?
*/
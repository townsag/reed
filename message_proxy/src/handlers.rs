// use std::error::Error;
// option tab is the command to prompt VSCode to suggest symbols
use tokio::sync::broadcast::{
    Sender, 
    error::RecvError,
};
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
use crate::repository::{Repository, RepoMessage};
use crate::broker::{BrokerMessage, Payload};
use crate::AppState;
use crate::state_machine::{Reader,  Writer};

enum ReaderEvent {
    ClosedByClient,
    ClosedByServer,
    ClientSyncStep1(StateVector),
    ServerSyncStep2(Update),
}

#[derive(Clone)]
pub struct UpdateMessage {
    client_id: u64,
    payload: Arc<Update>,
}

impl Routable for UpdateMessage {
    type Key = u64;
    fn key(&self) -> &Self::Key {
        return &self.client_id;
    }
}

pub async fn handler<R: Repository>(
    ws: WebSocketUpgrade, 
    Path((topic_id, user_id)): Path<(Uuid, Uuid)>,
    Query(client_id): Query<u64>,
    State(state): State<AppState<R>>,
) -> Response {
    ws.on_upgrade(move |socket| handle_socket(socket, topic_id, user_id, client_id, state))
}

fn decode_helper(
    result: Option<Result<Message, Error>>, 
    tx_ws_lifecycle: Sender<ReaderEvent>,
) -> Option<SyncMessage> {
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
    match result {
        Some(Ok(Message::Binary(bytes))) => SyncMessage::decode_v1(&bytes).ok(),
        Some(Ok(Message::Close(_))) | Some(Err(_)) | None => { tx_ws_lifecycle.send(ReaderEvent::ClosedByServer); None },
        // TODO: this will not work, we need some way to indicate that we should skip this message
        // right now we can only indicate use this message or close the websocket connection
        Some(Ok(_)) => None,
    }

}

// read messages from the websocket connection and send those messages to the broker
async fn read<R: Repository>(
    mut receiver: SplitStream<WebSocket>, 
    sender_broker: Sender<UpdateMessage>,
    tx_ws_lifecycle: Sender<ReaderEvent>,
    cancel_read_token: CancellationToken,
    repo: R,
    topic_id: Uuid,
    user_id: Uuid,
    client_id: u64,
) {
    // create an instance of the reader state
    let reader =  match Reader::new(repo, topic_id, user_id, client_id).await {
        Ok(reader) => reader,
        Err(e ) => {
            tx_ws_lifecycle.send(ReaderEvent::ClosedByServer);
            return;
        },
    };
    // send a message to the client indicating what updates we have already so that the client can send us
    // a bulk update of messages that we are missing 
    let reader_sv = reader.sync_step_one();
    tx_ws_lifecycle.send(ReaderEvent::ClientSyncStep1(reader_sv));

    // expect the client to send us a sync step one message including their version vector
    // or a sync step two message including the local updates that the server does not yet have
    let mut reader = loop {
        tokio::select! {
            result = receiver.next() => {
                match result {
                    Some(Ok(Message::Binary(bytes))) => {
                        // decode the sync message
                        match SyncMessage::decode_v1(&bytes) {
                            Ok(SyncMessage::SyncStep1(sv)) => {
                                // if it is a sync step one message, forward it to the writer
                                // TODO: may need some error handling code here in the case that 
                                // the client receiver has already been dropped
                                tx_ws_lifecycle.send(ReaderEvent::ClientSyncStep1(sv));
                            },
                            Ok(SyncMessage::SyncStep2(encoded_bulk_update)) => {
                                // if it is a sync step two message, process it at the reader then break
                                let bulk_update = match Update::decode_v1(&encoded_bulk_update) {
                                    Ok(update) => update,
                                    // TODO: log the error here so that we know when we receive bad
                                    // messages
                                    Err(e) => { continue; }
                                };
                                let (reader, bulk_update) = match reader.receive_bulk_update(bulk_update).await {
                                    Ok((r, u)) => (r, u),
                                    Err(_) => {
                                        tx_ws_lifecycle.send(ReaderEvent::ClosedByServer);
                                        return
                                    }
                                };
                                // write the bulk update to the broadcast channel
                                let update_message = UpdateMessage{
                                    client_id, payload: Arc::new(bulk_update),
                                };
                                if let Err(_ )= sender_broker.send(update_message) {
                                    let _ = tx_ws_lifecycle.send(ReaderEvent::ClosedByServer);
                                    return
                                }
                                break reader
                            },
                            // if it is an update message, reject it
                            Ok(SyncMessage::Update(_)) => {},
                            Err(e) => {
                                tx_ws_lifecycle.send(ReaderEvent::ClosedByServer);
                                return
                            }
                        };
                    }
                    Some(Ok(Message::Close(_close_frame))) => {
                        let _ = tx_ws_lifecycle.send(ReaderEvent::ClosedByClient);
                        return
                    },
                    // ignore Text, Ping, and Pong type messages
                    Some(Ok(_)) => {},
                    // ignore errors for now, these will be the messages that made it over the network
                    // but were lost at the OS level because of buffer overflow
                    // probably just close the websocket connection here so that it will be recreated
                    // on a server with less load and lost messages will be recovered in state handshake
                    Some(Err(e)) => {}
                    None => {
                        let _ = tx_ws_lifecycle.send(ReaderEvent::ClosedByClient);
                        return
                    }
                }
            }
            _ = cancel_read_token.cancelled() => {
                return;
            }
        }
    };

    // enter the hot path
    loop {
        tokio::select! {
            result = receiver.next() => {
                match result {
                    Some(Ok(Message::Binary(bytes))) => {
                        // decode the sync message
                        match SyncMessage::decode_v1(&bytes) {
                            Ok(SyncMessage::SyncStep1(sv)) => {
                                // if it is a sync step one message, forward it to the writer
                                // TODO: may need some error handling code here in the case that 
                                // the client receiver has already been dropped
                                tx_ws_lifecycle.send(ReaderEvent::ClientSyncStep1(sv));
                            },
                            Ok(SyncMessage::SyncStep2(_encoded_bulk_update)) => {
                                // if it is a sync step two message, ignore it
                            },
                            Ok(SyncMessage::Update(encoded_update)) => {
                                // if it is an update message, process it with the reader then forward
                                // it to other connected clients using the broker
                                let mut update = match Update::decode_v1(&encoded_update) {
                                    Ok(update) => update,
                                    Err(a) => { return },
                                };
                                update = match reader.receive_update(update).await {
                                    Ok(update) => update,
                                    Err(e) => { return }
                                };
                                let message = UpdateMessage {
                                    client_id,
                                    payload: Arc::new(update),
                                };
                                if let Err(_) = sender_broker.send(message) {
                                    tx_ws_lifecycle.send(ReaderEvent::ClosedByServer);
                                    return
                                }
                            },
                            Err(e) => {
                                tx_ws_lifecycle.send(ReaderEvent::ClosedByServer);
                                return
                            }
                        };
                    }
                    Some(Ok(Message::Close(_close_frame))) => {
                        let _ = tx_ws_lifecycle.send(ReaderEvent::ClosedByClient);
                        return
                    },
                    // ignore Text, Ping, and Pong type messages
                    Some(Ok(_)) => {},
                    // ignore errors for now, these will be the messages that made it over the network
                    // but were lost at the OS level because of buffer overflow
                    // probably just close the websocket connection here so that it will be recreated
                    // on a server with less load and lost messages will be recovered in state handshake
                    Some(Err(e)) => {}
                    None => {
                        let _ = tx_ws_lifecycle.send(ReaderEvent::ClosedByClient);
                        return
                    }
                }
            }
            _ = cancel_read_token.cancelled() => {
                return;
            }
        }
    }
}

// receive messages from the broker and send them to the websocket connection
async fn write(
    mut sender: SplitSink<WebSocket, Message>, 
    mut receiver_broker: WrappedReceiver<BrokerMessage>,
    mut rx_ws_lifecycle: OSReceiver<WebsocketLifecycleEvent>,
    cancel_read_token: CancellationToken,
    user_id: Uuid,
    client_id: u64,
) {
    // create an instance of the writer state
    loop {
        // we need to use this tokio select statement to prevent dangling write tasks
        // in the case that the websocket disconnects, the write task may not know that 
        // the websocket connection has already dropped if there are not other clients
        // sending messages for the write task to send over the channel
        // using a oneshot channel to communicate from the reader to the writer allows
        // us to know that the connection has been dropped as soon as we have read the
        // last message
        tokio::select! {
            message = receiver_broker.recv() => {
                match message {
                    Ok(msg) => {
                        if *msg.key() == user_id.hyphenated().to_string() {
                            // TODO: how come hyphenated does not consume self?
                            // TODO: check that this does what I think it does, can continue be used
                            //       inside of a tokio select statement
                            continue
                        }
                        let ws_message = match msg.payload {
                            Payload::Text(s) => Message::Text(s.into()),
                            Payload::Binary(b) => Message::Binary(b),
                        };
                        // TODO: batch read messages from the broker queue, use try receive
                        // TODO: batch send messages, concatenate many broker messages into one ws message
                        if let Err(_) = sender.send(ws_message).await {
                            // if we fail to send a message to the websocket client, return from the websocket
                            // write handler
                            cancel_read_token.cancel();
                            return
                        }
                    },
                    Err(RecvError::Closed) => {
                        // handle closure of the receiver from the broker
                        // this is an unrecoverable internal server error, we should close the connection
                        let _ = sender.send(Message::Close(Some(
                            CloseFrame { code: 1011, reason: "internal server error".into()},
                        )));
                        cancel_read_token.cancel();
                        return
                    },
                    Err(RecvError::Lagged(_)) => {
                        // TODO: this is the case in which we missed some messages sent to this topic
                        //       by some fast senders. We should read the missed messages from the
                        //       database so that we can catch up
                    }
                }
            }
            // use a mutable reference to the oneshot channel because otherwise each loop
            // would consume the channel
            ws_lifecycle_message = &mut rx_ws_lifecycle => {
                match ws_lifecycle_message {
                    // upon receiving a oneshot message that says message closed by the server
                    // we should send a closing frame and then close the connection
                    Ok(WebsocketLifecycleEvent::ClosedByClient) => {
                        // do nothing, the connection is already closed...
                        return
                    },
                    Ok(WebsocketLifecycleEvent::ClosedByServer) => {
                        // send a close frame over the websocket connection
                        let _ = sender.send(Message::Close(Some(
                            CloseFrame { code: 1011, reason: "internal server error".into()
                        })));
                        return
                    },
                    Err(_e) => {
                        // error indicates that the oneshot channel sender has been dropped
                        // this means that the task associated with reading from the websocket
                        // has stopped
                        // attempt to send a closing frame if possible
                        let _ = sender.send(Message::Close(Some(
                            CloseFrame { code: 1011, reason: "internal server error".into()
                        })));
                        return
                    },
                }
            }
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
    ) = state.broker.register(topic_id.to_string());
    // create a oneshot channel that the read task can use to send websocket lifecycle
    // events to the write task
    let (
        tx_ws_lifecycle, rx_ws_lifecycle
    ) = oneshot::channel();
    // create a cancellation token that write task can use to signal to the read task that 
    // it should shut down. We use a cancellation token here instead of a channel because 
    // we do not need to communicate the reason for cancellation
    let cancel_read_token = CancellationToken::new();
    // split the websocket into a message sender and a message receiver task
    // we will use the receiver to send messages from the client to the broker and the sender 
    // to send messages from the broker to the client
    let (sender_ws, receiver_ws) = socket.split();
    let mut set = JoinSet::new();
    // TODO: it would be interesting to see if the compiler will factor the user_id.to_string() call into one call
    //       that is assigned to a variable and then passed to each of the read and write task
    set.spawn(read(
        receiver_ws, sender_broker, tx_ws_lifecycle, cancel_read_token.clone(), state.repo, topic_id, user_id, client_id,
    ));
    set.spawn(write(
        sender_ws, receiver_broker, rx_ws_lifecycle, cancel_read_token, user_id, client_id,
    ));
    let _ = set.join_all().await;

    // TODO: we might want to do some book-keeping when the websocket connection closes
    //       return some sort of error value from the read and write tasks and use that 
    //       for book keeping
}
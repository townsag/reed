use tokio::sync::broadcast::{
    Sender, 
    error::RecvError,
};
use tokio::sync::oneshot::{
    self, Receiver as OSReceiver, Sender as OSSender
};
use tokio::task::JoinSet;
use tokio_util::sync::CancellationToken;

use axum::{
    extract::ws::{
        Message, 
        WebSocket, 
        WebSocketUpgrade, 
        CloseFrame,
    },
    extract::Path,
    extract::State,
    response::Response,
};
use futures_util::{
    SinkExt, 
    StreamExt, 
    stream::{SplitStream, SplitSink},
};
use crate::broker::WrappedReceiver;
use crate::{AppState, broker::{BrokerMessage, get_id}};

enum WebsocketLifecycleEvent {
    ClosedByClient,
    ClosedByServer,
}

pub async fn handler(
    ws: WebSocketUpgrade, 
    Path(topic_id): Path<String>,
    State(state): State<AppState>,
) -> Response {
    ws.on_upgrade(|socket| handle_socket(socket, topic_id, state))
}

// read messages from the websocket connection and send those messages to the broker
async fn read(
    mut receiver: SplitStream<WebSocket>, 
    sender_broker: Sender<BrokerMessage>,
    tx_ws_lifecycle: OSSender<WebsocketLifecycleEvent>,
    cancel_read_token: CancellationToken,
    connection_id: String,
) {
    loop {
        tokio::select! {
            result = receiver.next() => {
                match result {
                    Some(Ok(Message::Text(payload))) => {
                        let message = BrokerMessage {
                            connection_id: connection_id.clone(),
                            payload: payload.to_string(),
                        };
                        if let Err(_) = sender_broker.send(message) {
                            // receiving an error when trying to send to the broker indicates that 
                            // the broker receiver has been dropped and closed, we should send a 
                            // message indicating that there is an internal error and drop the 
                            // websocket connection
                            let _ = tx_ws_lifecycle.send(WebsocketLifecycleEvent::ClosedByServer);
                            return
                        }
                    },
                    // handle the explicit close message from the client
                    Some(Ok(Message::Close(_close_frame))) => {
                        // connection closed with closing frame
                        let _ = tx_ws_lifecycle.send(WebsocketLifecycleEvent::ClosedByClient);
                        return
                    },
                    Some(Ok(_)) => {
                        // ignore ping, pong, and binary type messages
                    },
                    // handle failed reads from the websocket and closed websocket connections
                    Some(Err(_e)) => {},
                    None => {
                        // connection closed without closing frame
                        let _ = tx_ws_lifecycle.send(WebsocketLifecycleEvent::ClosedByClient);
                        return
                    },
                }
            }
            _ = cancel_read_token.cancelled() => {
                return
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
    connection_id: String,
) {
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
                        if msg.connection_id == connection_id {
                            // TODO: check that this does what I think it does, can continue be used
                            //       inside of a tokio select statement
                            continue
                        }
                        // TODO: batch read messages from the broker queue, use try receive
                        // TODO: batch send messages, concatenate many broker messages into one ws message
                        if let Err(_) = sender.send(Message::Text(msg.payload.into())).await {
                            // if we fail to send a message to the websocket client, return from the websocket
                            // write handler
                            cancel_read_token.cancel();
                            return;
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
async fn handle_socket(socket: WebSocket, topic_id: String, state: AppState) {
    // generate a unique id for the websocket connection
    let connection_id = get_id();
    // register the websocket connection with the broker
    let (
        sender_broker, receiver_broker
    ) = state.broker.register(topic_id);
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
    set.spawn(read(receiver_ws, sender_broker, tx_ws_lifecycle, cancel_read_token.clone(), connection_id.to_string()));
    set.spawn(write(sender_ws, receiver_broker, rx_ws_lifecycle, cancel_read_token, connection_id.to_string()));

    let _ = set.join_all();

    // TODO: we might want to do some book-keeping when the websocket connection closes
    //       return some sort of error value from the read and write tasks and use that 
    //       for book keeping
}
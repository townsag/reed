use tokio::sync::mpsc::{Receiver, Sender};

use axum::{
    extract::ws::{Message, WebSocket, WebSocketUpgrade},
    extract::State,
    response::Response,
};
use futures_util::{SinkExt, StreamExt, stream::SplitStream};
use futures_util::stream::SplitSink;

use crate::{AppState, broker::{BrokerMessage, get_id}};

pub async fn handler(ws: WebSocketUpgrade, State(state): State<AppState>) -> Response {
    ws.on_upgrade(|socket| handle_socket(socket, state))
}


// read messages from the websocket connection and send those messages to the broker
async fn read(
    mut receiver: SplitStream<WebSocket>, 
    sender_broker: Sender<BrokerMessage>,
    connection_id: String,
) {
    while let Some(Ok(Message::Text(msg))) = receiver.next().await {
        sender_broker.send(BrokerMessage { 
            connection_id: connection_id.clone(), 
            payload: msg.to_string() 
        }).await.unwrap();
        // TODO: refactor this to have proper error handling
    }
}

// receive messages from the broker and send them to the websocket connection
async fn write(
    mut sender: SplitSink<WebSocket, Message>, 
    mut receiver_broker: Receiver<BrokerMessage>,
) {
    while let Some(msg) = receiver_broker.recv().await {
        sender.send(Message::Text(msg.payload.into())).await.unwrap();
    }
}

async fn handle_socket(socket: WebSocket, state: AppState) {
    // generate a unique id for the websocket connection
    let id = get_id();
    // register the websocket connection with the broker
    let (
        sender_broker, receiver_broker
    ) = state.broker.register(id.to_string());
    // split the websocket into a message sender and a message receiver task
    // we will use the receiver to send messages from the client to the broker and the sender 
    // to send messages from the broker to the client
    let (sender_ws, receiver_ws) = socket.split();
    
    tokio::spawn(write(sender_ws, receiver_broker));
    tokio::spawn(read(receiver_ws, sender_broker, id.to_string()));

    // TODO: we might want to do some book-keeping when the websocket connection closes
    //       return some sort of error value from the read and write tasks and use that 
    //       for book keeping
}
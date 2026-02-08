use axum::{
    Router,
    routing::{get, any},
    extract::ws::{WebSocketUpgrade, WebSocket},
    response::Response,
};
use tokio;


async fn handler(ws: WebSocketUpgrade) -> Response {
    ws.on_upgrade(handle_socket)
}

async fn handle_socket(mut socket: WebSocket) {
    // this is odd syntax, while the message we receive over the websocket is a dataframe 
    // process the message
    // recv returns an optional result type, this means that there could be some or none
    // inside of some there could be either a message or an error
    // what happens next? is there any indication that the websocket is closed when we complete
    // the while loop?
    while let Some(msg) = socket.recv().await {
        // if let allows us to write a shorter version of a match statement
        // if msg is the Ok variant of the result enum then we destructure the value out of the 
        // result enum and assign that value to msg which shadows the outer value of msg
        // this value is then returned to msg, which also shadows the outer value of msg
        let msg = if let Ok(msg) = msg {
            msg
        } else {
            // in all cases other than the Ok variant of the result enum
            // we return 
            return;
        };

        if socket.send(msg).await.is_err() {
            return
        }
    }
}

#[tokio::main]
async fn main() {
    let app = Router::<()>::new();
    // if we want this to be multiline definition then we have to reassign the variable app because
    // self moves into the .route method
    let app = app.route("/", get(|| async {"hello world"}));
    let app = app.route("/ws", any(handler));

    let listener = tokio::net::TcpListener::bind("0.0.0.0:3000").await.unwrap();
    axum::serve(listener, app).await.unwrap();
}
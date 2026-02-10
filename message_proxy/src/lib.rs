// define a module for handlers
mod handlers;
// define a module for the message broker
pub mod broker;

use axum::{
    Router,
    routing::{any,get},
};
use crate::handlers::handler;



pub async fn run() {
    let app = Router::<()>::new();
    // if we want this to be multiline definition then we have to reassign the variable app because
    // self moves into the .route method
    let app = app.route("/", get(|| async {"hello world"}));
    let app = app.route("/ws", any(handler));

    let listener = tokio::net::TcpListener::bind("0.0.0.0:3000").await.unwrap();
    axum::serve(listener, app).await.unwrap();
}
// define a module for handlers
mod handlers;
// define a module for the message broker
pub mod broker;

use axum::{
    Router,
    routing::{any,get},
};
use crate::{broker::{Broker, BrokerBuilder, BrokerMessage}, handlers::handler};
use tracing::{
    event,
    Level,
};
use tracing_subscriber::{
    filter::LevelFilter,
};


#[derive(Clone)]
struct AppState {
    broker: Broker<BrokerMessage>,
}

pub async fn run() {
    tracing_subscriber::fmt().with_max_level(LevelFilter::DEBUG).init();
    
    let broker = BrokerBuilder::default().build::<BrokerMessage>();
    // when creating a router the state type parameter indicates the type of the state
    // struct that has not yet been passed to the router (using .with_state(: S))
    // this is why we don't parameterize the with_state function, that would indicate
    // that there is still a state that needs to be passed to the router
    let app = Router::new()
        .route("/", get(|| async {"hello world"}))
        .route("/ws/{topic_id}", any(handler))
        .with_state(AppState { broker });

    let listener = tokio::net::TcpListener::bind("0.0.0.0:3000").await.unwrap();
    event!(Level::INFO, "starting server on port 3000");
    axum::serve(listener, app).await.unwrap();
}
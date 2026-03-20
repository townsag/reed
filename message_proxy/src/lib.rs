// define a module for handlers
mod handlers;
// define a module for the message broker
pub mod broker;
pub mod config {
    pub mod postgres;
}
mod repository;

use axum::{
    Router,
    routing::{any,get},
};
use crate::{
    broker::{Broker, BrokerBuilder, BrokerMessage}, 
    handlers::handler, 
    repository::{Repository, postgres::PgRepo},
    config::postgres,
};
use tracing::{
    event,
    Level,
};
use tracing_subscriber::{
    filter::LevelFilter,
};


#[derive(Clone)]
struct AppState<R: Repository> {
    broker: Broker<BrokerMessage>,
    // TODO: learn more about the difference between dynamic dispatch and static dispatch
    // for now it seemed prudent to use static dispatch here because we are always going to know the
    // repository type at compile time and we are not going to use multiple repository types 
    // at the same time
    repo: R,
}

pub async fn run() {
    tracing_subscriber::fmt().with_max_level(LevelFilter::DEBUG).init();
    let pool = match postgres::build_postgres_pool().await {
        Ok(pool) => pool,
        Err(err) => {
            tracing::error!("failed to build postgres pool with error: {err}");
            return;
        }
    };
    let broker = BrokerBuilder::default().build::<BrokerMessage>();
    // when creating a router the state type parameter indicates the type of the state
    // struct that has not yet been passed to the router (using .with_state(: S))
    // this is why we don't parameterize the with_state function, that would indicate
    // that there is still a state that needs to be passed to the router
    let app = Router::new()
        .route("/", get(|| async {"hello world"}))
        .route("/ws/{topic_id}/{user_id}", any(handler))
        .with_state(AppState::<PgRepo>{ broker, repo: PgRepo::new(pool) });

    let listener = tokio::net::TcpListener::bind("0.0.0.0:3000").await.unwrap();
    event!(Level::INFO, "starting server on port 3000");
    axum::serve(listener, app).await.unwrap();
}
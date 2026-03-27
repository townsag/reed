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
use yrs::{Doc, Text, Transact, Update, updates::decoder::Decode};


fn try_yrs() {
    // create a local copy of the document
    let doc_l = Doc::new();
    let text_l = doc_l.get_or_insert_text("content");
    // create a remote copy of the document
    // let doc_r = Doc::new();
    // let text_r = doc_r.get_or_insert_text("content");
    // make a modification to the local document
    {
        let mut txn1 = doc_l.transact_mut();
        // observe the changes to the local document
        text_l.insert(&mut txn1, 0, "Hello");
        let updates = txn1.encode_update_v1();
        // try to decode and debug print the updates so that they make some sense
        match Update::decode_v1(&updates) {
            Ok(update) => print!("{:#?}", update),
            Err(e) => print!("failed to decode with error: {}", e),
        };
    }
}


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
    // try_yrs();
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



/*
https://docs.rs/yrs/latest/yrs/trait.ReadTxn.html#method.encode_diff
fn encode_diff<E: Encoder>(&self, state_vector: &StateVector, encoder: &mut E)
Encodes the difference between remote peer state given its state_vector and the state of a current local peer.

Differences between alternative methods
Self::encode_state_as_update encodes full document state including pending updates and entire delete set.
Self::encode_diff encodes only the difference between the current state and the given state vector, including entire delete set. Pending updates are not included.
TransactionMut::encode_update encodes only inserts and deletes made within the scope of the current transaction.


https://docs.rs/yrs/latest/yrs/struct.TransactionMut.html#method.encode_update



existing codemirror plugins for markdown editing
- codemirror-rich-markdoc (segphault) — token hiding + block widgets for CM6, good reference
  even if you don't use it directly
- codemirror-markdown-hybrid — the newer line-level approach, very minimal API 
  (hybridMarkdown({ theme: 'light' }))

- reference the "sync protocol" when creating the client - server integration
    - https://github.com/yjs/y-protocols/blob/master/PROTOCOL.md

- Approach:
    - from the perspective of the server:
        - keep a record of the last know state vector of the client
        - send all messages that are proxied from other clients that have a state vector 
          that is higher than the last know state vector of the client
        - periodically receive the state vector from the client
        - if the client notices that there are missing messages received:
            - read all messages that follow the last know state vector of the client
              and send those to the client as one update vector
            - we do not want to leave the hot path but if we have to leave the hot 
              path it should be very fast to get back on the hot path
        - we can think about eventual consistency later after it works in the hot path
        - modify the websocket read handler so that it is assumed that we are reading
          yjs / yrs binary messages instead of text format messages
            - user_id and message_offset will be encoded into the message contents not the query param
              and the counter
            - notably we need to parse these things and write them to the database 
    - from the perspective of the client
        - use Yjs and the y-codemirror.next editor with the codemirror-markdown-hybrid extension
    - model the write handler and the read handler as communicating state machines with channels
        - they have to communicate between each other
            - errors, failures, connection closed
            - state vector updates
- More Detail on the relationship between the client and the server:
    - we need to assume that the network is unreliable
        - messages that we send can fail, in that case the state vector needs to be renegotiated
    - messages from other clients are made persistent before they are available to be proxied
        - persistent can mean written to db or durable queue
        - this means that we can be sure that operation messages from other clients that 
          we drop from the broadcast queue can be read from the database
    - what should the client server relationship look like
        - if it is detected that the client is behind the server by more than just the 
          message that is currently being processed, switch from operation sync mode to
          state sync mode
        - operation sync mode 
            - proxy all incoming messages from other connections to the client
            - keep track of the last confirmed state vector of the client
            - keep track of our optimistic guess of the state vector of the client
              based on its last confirmed state vector and the operation messages
              that are already inflight 
            - when we detect a message that is to be proxied that is more than one offset
              higher than the optimistic guess offset of the client, switch from operation
              sync mode to state sync mode
                - the client should periodically send us their state vector (every few seconds)
                  in a heartbeat
                - use the last know state vector of the client to create the state vector
            - when we receive a message from the client indicating that they have missed some
              messages, then enter state sync mode
                - this message will include the clients actual known state vector
        - state sync mode
            - receive the state vector from the client or use the last know state vector 
              from the client
            - use the state vector to create a diff message
            - send the diff message and record the new optimistic state vector
            - switch back to operation sync mode
*/
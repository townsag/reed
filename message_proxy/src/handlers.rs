// option tab is the command to prompt VSCode to suggest symbols
use tokio::sync::oneshot;
use std::sync::Arc;
use std::time::Instant;
use tokio::task::JoinSet;
use tokio_util::sync::CancellationToken;
use uuid::Uuid;
use axum::{
    extract::ws::{
        WebSocket, 
        WebSocketUpgrade,
    },
    extract::Path,
    extract::Query,
    extract::State,
    response::Response,
};
use futures_util::{
    StreamExt, 
};
use yrs::{
    sync::protocol::SyncMessage,
};
use crate::{v1::operations::Operation};
use crate::repository::{Repository, RepoError};
use crate::AppState;
use crate::config::otel::{
    MetricsWS,
};
use thiserror::Error;
use serde;
use tracing::{Level, event};
mod read;
mod write;

#[derive(Debug)]
enum ReaderEvent {
    ClientSyncStep1(Vec<u8>, Instant),
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
    // TODO: if we find that the cost of encoding and decoding is too expensive we should
    // find an alternative pattern
    Valid(SyncMessage, Instant),
    Skip,
    Failure,
}

struct WebsocketHandler<R: Repository> {
    topic_id: Uuid,
    user_id: Uuid,
    client_id: u64,
    repo: R,
    metrics_ws: MetricsWS,
}

impl <R: Repository> WebsocketHandler<R> {
    fn new(
        topic_id: Uuid,
        user_id: Uuid,
        client_id: u64,
        repo: R,
        metrics_ws: MetricsWS,
    ) -> Self {
        WebsocketHandler { topic_id, user_id, client_id, repo, metrics_ws }
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
    ReaderToBroadcastSendError(#[from] tokio::sync::broadcast::error::SendError<Arc<Operation>>),
}

// TODO: parse the user id from a query parameter with a jwt in it
async fn handle_socket<R: Repository>(
    socket: WebSocket,
    topic_id: Uuid,
    user_id: Uuid,
    client_id: u64,
    state: AppState<R>,
) {
    let _guard = state.metrics_ws.ws_lifecycle_guard();
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
        topic_id, user_id, client_id, state.repo, state.metrics_ws.clone(),
    ));
    let handler_read = Arc::clone(&websocket_handler);
    let handler_write = Arc::clone(&websocket_handler);
    let cancel_token_read = cancel_token.clone();
    let cancel_token_write = cancel_token.clone();

    let mut set = JoinSet::new();

    set.spawn(async move { handler_read.read(websocket_receiver, broker_sender, sync_sender, cancel_token_read).await });
    set.spawn(async move { handler_write.write(websocket_sender, broker_receiver, sync_receiver, cancel_token_write).await });
    while let Some(result) = set.join_next().await {
        if let Ok(Err(e)) = result {
            event!(
                name: "task_finished_error",
                Level::ERROR,
                error=%e,
                client_id_src=client_id,
                client_id_dst=client_id,
            )
        }
        // TODO: this is messy, we are not yet sure which task is returning this error
        // so we could be calling cancel on the token after the read task has already returned 
        cancel_token.cancel();
    }

    // TODO: we might want to do some book-keeping when the websocket connection closes
    //       return some sort of error value from the read and write tasks and use that 
    //       for book keeping
}

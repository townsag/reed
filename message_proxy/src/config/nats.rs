use async_nats::{
    Client,
    ConnectError,
};
use std::env;

pub async fn build_nats_client() -> Result<Client, ConnectError> {
    let host = env::var("NATS_CORE_HOST").unwrap_or("nats".into());
    let port = env::var("NATS_CORE_PORT").unwrap_or("4222".into());
    async_nats::connect(format!("{}:{}", host, port)).await
}
use std::env;

use sqlx::{
    Postgres, postgres::{
        PgConnectOptions, PgPoolOptions
    },
    Error
};
use sqlx_tracing::{Pool};

const MAX_CONNECTIONS: u32 = 10;

pub async fn build_postgres_pool() -> Result<Pool<Postgres>, Error> {
    // read postgres database connection information from env with defaults
    let host = env::var("POSTGRES_HOST").unwrap_or("localhost".to_string());
    let username = env::var("POSTGRES_USER").unwrap_or("admin".to_string());
    let password = env::var("POSTGRES_PASS").unwrap_or("password".to_string());
    let database = env::var("POSTGRES_DATABASE").unwrap_or("postgres".to_string());

    let options = PgConnectOptions::new()
        .host(&host)
        .port(5432)
        .username(&username)
        .password(&password)
        .database(&database)
        .ssl_mode(sqlx::postgres::PgSslMode::Disable);
    let pool = PgPoolOptions::new().max_connections(MAX_CONNECTIONS).connect_with(options).await?;

    Ok(Pool::from(pool))
}
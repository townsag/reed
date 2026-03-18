use sqlx::{Pool, Postgres, postgres::{
    PgConnectOptions, PgPoolOptions
},
Error};


const MAX_CONNECTIONS: u32 = 10;

async fn build_postgres_pool() -> Result<Pool<Postgres>, Error> {
    let options = PgConnectOptions::new()
        .host("localhost")
        .port(5432)
        .username("admin")
        .password("password")
        .ssl_mode(sqlx::postgres::PgSslMode::Disable);
    PgPoolOptions::new().max_connections(MAX_CONNECTIONS).connect_with(options).await
}
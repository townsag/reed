use sqlx::{Pool, Postgres};
use std::clone::Clone;

use crate::repository::{Repository, RepoError, ErrorKind, RepoMessage};

// we want the PgRepo to be easily copyable so that it can be shared across async threads
// create the struct such that the elements of the struct are easily copyable and the
// derive clone on the struct
#[derive(Clone)]
pub struct PgRepo {
    pool: Pool<Postgres>,
}

impl PgRepo {
    pub fn new(pool: Pool<Postgres>) -> Self {
        return PgRepo { pool }
    }
}

impl Repository for PgRepo {
    async fn write_message(&self, message: RepoMessage) -> Result<(), RepoError> {
        let res = sqlx::query!(
            "INSERT into messages (topic_id, user_id, message_offset, content) VALUES ($1, $2, $3, $4);",
            message.topic_id,
            message.user_id,
            message.message_offset,
            message.content,
        ).execute(&self.pool)
        .await;
        match res {
            Ok(_) => Ok(()),
            Err(e @ sqlx::error::Error::InvalidArgument(_)) => {
                Err(RepoError { kind: ErrorKind::SchemaMismatch, source: Box::new(e)})
            },
            Err(e) => Err(RepoError { kind: ErrorKind::FailedWrite, source: Box::new(e)}),
        }
    }
    async fn write_messages(&self, messages: Vec<RepoMessage>) -> Result<(), RepoError> {
        Ok(())
    }
}
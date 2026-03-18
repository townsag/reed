use sqlx::{Pool, Postgres};
use std::clone::Clone;

use crate::repository::{Repository, RepoError};

// we want the PgRepo to be easily copyable so that it can be shared across async threads
// create the struct such that the elements of the struct are easily copyable and the
// derive clone on the struct
#[derive(Clone)]
pub struct PgRepo {
    pool: Pool<Postgres>,
}

impl PgRepo {
    fn new(pool: Pool<Postgres>) -> Self {
        return PgRepo { pool }
    }
}

impl Repository for PgRepo {
    fn write_message(&self, message: &str) -> Result<(), RepoError> {
        Ok(())
    }
    fn write_messages(&self, messages: Vec<&str>) -> Result<(), RepoError> {
        Ok(())
    }
}
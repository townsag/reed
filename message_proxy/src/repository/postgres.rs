use sqlx::{Pool, Postgres};
use std::clone::Clone;

use crate::repository::{Repository, RepoError, ErrorKind, /* RepoMessage */ };

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
    // async fn write_message(&self, message: RepoMessage) -> Result<(), RepoError> {
    //     let res = sqlx::query!(
    //         "INSERT into messages (topic_id, user_id, message_offset, content) VALUES ($1, $2, $3, $4);",
    //         message.topic_id,
    //         message.user_id,
    //         message.message_offset,
    //         message.content,
    //     ).execute(&self.pool)
    //     .await;
    //     match res {
    //         Ok(_) => Ok(()),
    //         Err(e @ sqlx::error::Error::InvalidArgument(_)) => {
    //             Err(RepoError { kind: ErrorKind::SchemaMismatch, source: Box::new(e)})
    //         },
    //         Err(e) => Err(RepoError { kind: ErrorKind::FailedWrite, source: Box::new(e)}),
    //     }
    // }
    async fn write_operation(
        &self,
        topic_id: uuid::Uuid,
        user_id: uuid::Uuid,
        client_id: u64,
        offset: u32,
        payload: &[u8],
    ) -> Result<(),RepoError> {
        let res = sqlx::query!(
            "INSERT INTO operations (topic_id, user_id, client_id, operation_offset, payload) 
            VALUES ($1, $2, $3, $4, $5)",
            topic_id,
            user_id,
            client_id as i64,
            // this will wrap if the u64 overflows the i64
            // I don't think this will be an issue though because the 
            // client id is created from a u32
            offset as i64,
            payload,
        ).execute(&self.pool).await;
        match res {
            Ok(_) => Ok(()),
            Err(e @ sqlx::error::Error::InvalidArgument(_)) => {
                Err(RepoError { kind: ErrorKind::SchemaMismatch, source: Box::new(e)})
            },
            Err(e) => Err(RepoError { kind: ErrorKind::FailedWrite, source: Box::new(e)}),
        }
    }
    async  fn read_operations_after(
        &self, state_vector: &[(u64,u32)], topic_id: uuid::Uuid,
    ) -> Result<Vec<Vec<u8> > ,RepoError> {
        let operations = sqlx::query!(
            "WITH version_vector AS(
                SELECT * FROM UNNEST($1::bigint[], $2::bigint[])
                AS t(client_id, min_offset)
            )
            SELECT o.* 
            FROM operations o 
            LEFT JOIN version_vector
                ON o.client_id = version_vector.client_id
            WHERE (
                version_vector.client_id IS NULL
                OR o.operation_offset > version_vector.min_offset
            ) AND o.topic_id = $3",
            &state_vector.iter().map(|(k, _)| *k as i64).collect::<Vec<i64>>(),
            &state_vector.iter().map(|(_, v)| *v as i64).collect::<Vec<i64>>(),
            topic_id,
        ).fetch_all(&self.pool).await;
        match operations {
            Ok(operations) => {
                Ok(operations.into_iter().map(|op| op.payload).collect())
            },
            Err(e @ sqlx::error::Error::InvalidArgument(_)) => {
                Err(RepoError { kind: ErrorKind::SchemaMismatch, source: Box::new(e)})
            },
            Err(e) => Err(RepoError { kind: ErrorKind::FailedWrite, source: Box::new(e)}),
        }
    }
    async  fn read_last_received_offset(&self, client_id:u64) -> Result<Option<u32>,RepoError> {
        let result = sqlx::query!(
            "SELECT MAX(operation_offset) AS max_offset FROM operations
            WHERE client_id =$1",
            client_id as i64,
        ).fetch_one(&self.pool).await;
        match result{
            Ok(record) => {
                Ok(record.max_offset.map(|o| {o as u32}))
            },
            Err(e @ sqlx::error::Error::InvalidArgument(_)) => {
                Err(RepoError { kind: ErrorKind::SchemaMismatch, source: Box::new(e)})
            },
            Err(e) => Err(RepoError { kind: ErrorKind::FailedWrite, source: Box::new(e)}),
        }
    }
}

#[cfg(test)]
mod tests {
    use std::vec;

    use uuid::Uuid;
    use crate::repository::{
        RepoError, Repository,
        postgres::{PgRepo},
    };
    use sqlx::{Pool, Postgres};

    #[sqlx::test]
    async fn test_write_read_all_operations(pool: Pool<Postgres>) -> Result<(), RepoError> {
        let (topic_id, user_id, client_id) = (Uuid::nil(), Uuid::nil(), 1 as u64);
    
        let repo = PgRepo::new(pool);
        let operation_1: Vec<u8> = vec![1,2,3];
        let operation_2: Vec<u8> = vec![4,5,6];
        // write two operations to the database
        let _ = repo.write_operation(
            topic_id, user_id, client_id, 0, &operation_1
        ).await?;
        let _ = repo.write_operation(
            topic_id, user_id, client_id, 1, &operation_2,
        ).await?;
        // read those operations
        let state_vector: Vec<(u64, u32)> = vec![];
        let operations = repo.read_operations_after(&state_vector, topic_id).await?;
        assert_eq!(operations.len(), 2);
        assert_eq!(operations[0], operation_1);
        assert_eq!(operations[1], operation_2);
        Ok(())
    }
    #[sqlx::test]
    async fn test_write_read_operations_after_offset(pool: Pool<Postgres>) -> Result<(), RepoError> {
        let (topic_id, user_id, client_id) = (Uuid::nil(), Uuid::nil(), 1 as u64);
    
        let repo = PgRepo::new(pool);
        let operation_1: Vec<u8> = vec![1,2,3];
        let operation_2: Vec<u8> = vec![4,5,6];
        // write two operations to the database
        let _ = repo.write_operation(
            topic_id, user_id, client_id, 0, &operation_1
        ).await?;
        let _ = repo.write_operation(
            topic_id, user_id, client_id, 1, &operation_2,
        ).await?;

        let state_vector: Vec<(u64, u32)> = vec![(client_id,0)];
        let operations = repo.read_operations_after(&state_vector, topic_id).await?;
        assert_eq!(operations.len(), 1);
        assert_eq!(operations[0], operation_2);
        Ok(())
    }
    #[sqlx::test]
    async fn test_write_read_operations_multi_client(pool: Pool<Postgres>) -> Result<(), RepoError> {
        let repo = PgRepo::new(pool);
        // the goal of this test is to validate that the version vector logic works when 
        // we have both missed operations from clients that we have seen operations for, and
        // missed operations from clients that we have not seen operations for
        let (topic_id, user_id) = (Uuid::nil(), Uuid::nil());
        let client_1: u64 = 1;
        let client_2: u64 = 2;
        let operations = vec![
            (client_1, 0u32, vec![1u8,2,3]), 
            (client_1, 1u32, vec![4u8,5,6]),
            (client_2, 0u32, vec![7u8,8,9]),
        ];
        for elem in operations {
            repo.write_operation(
                topic_id, user_id, elem.0, elem.1, &elem.2,
            ).await?;
        }

        let version_vector = vec![(1u64, 0u32)];
        let read = repo.read_operations_after(&version_vector, topic_id).await?;
        assert_eq!(read.len(), 2);
        assert!(read.contains(&vec![4u8,5,6]));
        assert!(read.contains(&vec![7u8,8,9]));

        Ok(())
    }
    #[sqlx::test]
    async fn test_write_read_last_offset(pool: Pool<Postgres>) -> Result<(), RepoError> {
        let (topic_id, user_id, client_id) = (Uuid::nil(), Uuid::nil(), 1 as u64);
    
        let repo = PgRepo::new(pool);
        let operation_1: Vec<u8> = vec![1,2,3];
        // write two operations to the database
        let _ = repo.write_operation(
            topic_id, user_id, client_id, 0, &operation_1
        ).await?;

        let last_offset = repo.read_last_received_offset(client_id).await?;

        assert_eq!(last_offset, Some(0));
        Ok(())
    }
    #[sqlx::test]
    async fn test_last_offset_not_found(pool: Pool<Postgres>) -> Result<(), RepoError> {
        let repo = PgRepo::new(pool);
        let last_offset = repo.read_last_received_offset(1).await?;
        assert_eq!(last_offset, None);
        Ok(())
    }
}
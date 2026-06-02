use opentelemetry::{InstrumentationScope, KeyValue, global, metrics::Histogram};
use sqlx::{Postgres};
use sqlx_tracing::Pool;
use tokio::time::Instant;
use std::clone::Clone;
use tracing::instrument;

use crate::repository::{Repository, RepoError, ErrorKind, /* RepoMessage */ };


// follow the convention defined here:
// https://opentelemetry.io/docs/specs/semconv/db/database-metrics/#metric-dbclientoperationduration
#[derive(Clone)]
struct MetricsPGRepo {
    db_client_operations_duration: Histogram<f64>,
}

// we want the PgRepo to be easily copyable so that it can be shared across async threads
// create the struct such that the elements of the struct are easily copyable and the
// derive clone on the struct
#[derive(Clone)]
pub struct PgRepo {
    pool: Pool<Postgres>,
    metrics_pg: MetricsPGRepo,
}

impl PgRepo {
    pub fn new(pool: Pool<Postgres>) -> Self {
        let scope = InstrumentationScope::builder("repository.pg")
            .with_version("v0.0.1")
            .build();
        let meter = global::meter_with_scope(scope);
        let db_client_operations_duration = meter
            .f64_histogram("db.client.operation.duration")
            .with_description("duration of database operations")
            .with_boundaries(vec![ 0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1.0, 5.0, 10.0 ])
            .with_unit("second")
            .build();
        // {namespace}.{component}.{action_or_measurement}
        // let write_update_message_duration = meter
        //     .f64_histogram("repository.pg.write-update-message-duration")
        //     .with_description("duration of time taken to write an update sync message")
        //     .with_unit("ms")
        //     // Setting boundaries is optional. By default, the boundaries are set to
        //     // [0.0, 5.0, 10.0, 25.0, 50.0, 75.0, 100.0, 250.0, 500.0, 750.0, 1000.0, 2500.0, 5000.0, 7500.0, 10000.0]
        //     .build();
        // let read_last_offset_duration = meter
        //     .f64_histogram("repository.pg.read-last-offset-duration")
        //     .with_description("duration of time taken to read the last offset written for a client on a topic")
        //     .with_unit("ms")
        //     .build();
        // let read_operations_after_sv_duration = meter
        //     .f64_histogram("repository.pg.read-operations-after-sv")
        //     .with_description("duration of time taken to read operations after a state vector")
        //     .with_unit("ms")
        //     .build();
        let metrics_pg = MetricsPGRepo {
            db_client_operations_duration,
        };

        return PgRepo { pool, metrics_pg }
    }
}

impl Repository for PgRepo {
    #[instrument(skip(self,payload))]
    async fn write_operation(
        &self,
        topic_id: uuid::Uuid,
        user_id: uuid::Uuid,
        client_id: u64,
        offset: u32,
        payload: &[u8],
        // TODO: allow the calling code to specify if this operation is a sync step two 
        // operation or an update type operation. This can be included in the attributes
        // of the histogram
    ) -> Result<(),RepoError> {
        let start = Instant::now();
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
        let ret = match res {
            Ok(_) => Ok(()),
            Err(e @ sqlx::error::Error::InvalidArgument(_)) => {
                Err(RepoError { kind: ErrorKind::SchemaMismatch, source: Box::new(e)})
            },
            Err(e) => Err(RepoError { kind: ErrorKind::FailedWrite, source: Box::new(e)}),
        };

        self.metrics_pg.db_client_operations_duration.record(
            start.elapsed().as_secs_f64(), 
            &[
                KeyValue::new("db.system.name", "postgresql"),
                KeyValue::new("db.operation.name", "INSERT"),
                KeyValue::new("db.collection.name", "operations"),
                // TODO: this should not be a magic string, we should read this upon creating the repo
                KeyValue::new("db.namespace", "message_proxy"),
                KeyValue::new("db.query.summary", "write the contents of an operation sync message"),
                // TODO: extend this to read database returned error information on failure
                // TODO: would prefer to do this without making any heap allocations
                // KeyValue::new("error.type", "connection_timeout"),
                // KeyValue::new("db.response.status_code", ),
            ],
        );

        ret
    }
    #[instrument(skip(self,state_vector),fields(count_updates))]
    async  fn read_operations_after(
        &self, state_vector: &[(u64,u32)], topic_id: uuid::Uuid,
    ) -> Result<Vec<Vec<u8>> ,RepoError> {
        let start = Instant::now();
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
        let ret = match operations {
            Ok(operations) => {
                tracing::Span::current().record("count_updates", operations.len());
                Ok(operations.into_iter().map(|op| op.payload).collect())
            },
            Err(e @ sqlx::error::Error::InvalidArgument(_)) => {
                Err(RepoError { kind: ErrorKind::SchemaMismatch, source: Box::new(e)})
            },
            Err(e) => Err(RepoError { kind: ErrorKind::FailedWrite, source: Box::new(e)}),
        };

        self.metrics_pg.db_client_operations_duration.record(
            start.elapsed().as_secs_f64(), 
            &[
                KeyValue::new("db.system.name", "postgresql"),
                KeyValue::new("db.operation.name", "SELECT"),
                KeyValue::new("db.collection.name", "operations"),
                // TODO: this should not be a magic string, we should read this upon creating the repo
                KeyValue::new("db.namespace", "message_proxy"),
                KeyValue::new("db.query.summary", "read the operations with a happens after relationship given a state vector"),
                // TODO: extend this to read database returned error information on failure
                // TODO: would prefer to do this without making any heap allocations
                // KeyValue::new("error.type", "connection_timeout"),
                // KeyValue::new("db.response.status_code", ),
            ],
        );

        ret
    }
    #[instrument(skip(self),fields(last_received_offset))]
    async  fn read_last_received_offset(
        &self,
        topic_id: uuid::Uuid,
        client_id: u64,
    ) -> Result<Option<u32>,RepoError> {
        let start = Instant::now();
        let result = sqlx::query!(
            "SELECT MAX(operation_offset) AS max_offset FROM operations
            WHERE topic_id =$1 AND client_id =$2",
            topic_id,
            client_id as i64,
        ).fetch_one(&self.pool).await;
        let ret = match result{
            Ok(record) => {
                tracing::Span::current().record("last_received_offset", record.max_offset);
                Ok(record.max_offset.map(|o| {o as u32}))
            },
            Err(e @ sqlx::error::Error::InvalidArgument(_)) => {
                Err(RepoError { kind: ErrorKind::SchemaMismatch, source: Box::new(e)})
            },
            Err(e) => Err(RepoError { kind: ErrorKind::FailedWrite, source: Box::new(e)}),
        };

        self.metrics_pg.db_client_operations_duration.record(
            start.elapsed().as_secs_f64(), 
            &[
                KeyValue::new("db.system.name", "postgresql"),
                KeyValue::new("db.operation.name", "SELECT"),
                KeyValue::new("db.collection.name", "operations"),
                // TODO: this should not be a magic string, we should read this upon creating the repo
                KeyValue::new("db.namespace", "message_proxy"),
                KeyValue::new("db.query.summary", "read the offset of the last received operation given a client and topic"),
                // TODO: extend this to read database returned error information on failure
                // TODO: would prefer to do this without making any heap allocations
                // KeyValue::new("error.type", "connection_timeout"),
                // KeyValue::new("db.response.status_code", ),
            ],
        );

        ret
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
    use sqlx_tracing::{Pool as TracedPool};

    #[sqlx::test]
    async fn test_write_read_all_operations(pool: Pool<Postgres>) -> Result<(), RepoError> {
        let (topic_id, user_id, client_id) = (Uuid::nil(), Uuid::nil(), 1 as u64);
    
        let repo = PgRepo::new(TracedPool::from(pool));
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
    
        let repo = PgRepo::new(TracedPool::from(pool));
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
        let repo = PgRepo::new(TracedPool::from(pool));
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
    
        let repo = PgRepo::new(TracedPool::from(pool));
        let operation_1: Vec<u8> = vec![1,2,3];
        // write two operations to the database
        let _ = repo.write_operation(
            topic_id, user_id, client_id, 0, &operation_1
        ).await?;

        let last_offset = repo.read_last_received_offset(topic_id, client_id).await?;

        assert_eq!(last_offset, Some(0));
        Ok(())
    }
    #[sqlx::test]
    async fn test_last_offset_not_found(pool: Pool<Postgres>) -> Result<(), RepoError> {
        let repo = PgRepo::new(TracedPool::from(pool));
        let last_offset = repo.read_last_received_offset(Uuid::nil(), 1).await?;
        assert_eq!(last_offset, None);
        Ok(())
    }
    #[sqlx::test]
    async fn test_other_topic_client(pool: Pool<Postgres>) -> Result<(), RepoError> {
        let repo = PgRepo::new(TracedPool::from(pool));
        let (topic_id_a, topic_id_b, user_id, client_id) = (
            Uuid::new_v4(), Uuid::new_v4(), Uuid::nil(), 1 as u64,
        );
        let operation_1: Vec<u8> = vec![1,2,3];
        // write an operation at offset one on the first topic
        repo.write_operation(topic_id_a, user_id, client_id, 0, &operation_1).await?;
        // verify that the operation offset can be read on the first topic
        let result = repo.read_last_received_offset(topic_id_a, client_id).await?;
        assert_eq!(result, Some(0));
        // try to read that operation on the second topic
        let result = repo.read_last_received_offset(topic_id_b, client_id).await?;
        // verify that the read operation is not found
        assert_eq!(result, None);

        Ok(())
    }
}
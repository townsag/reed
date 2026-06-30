use opentelemetry::{InstrumentationScope, KeyValue, global, metrics::Histogram};
use sqlx::postgres::types::PgRange;
use sqlx::{self, Postgres};
use sqlx_tracing::Pool;
use std::{
    clone::Clone,
    collections::HashMap,
    ops::{Bound, Range},
};
use tokio::time::Instant;
use tracing::instrument;

use crate::repository::{ErrorKind /* RepoMessage */, RepoError, Repository};

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
            .with_boundaries(vec![0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1.0, 5.0, 10.0])
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

        return PgRepo { pool, metrics_pg };
    }
}

#[derive(sqlx::Type)]
#[sqlx(transparent)]
struct PgRangeWrapper(PgRange<i64>);

impl From<PgRangeWrapper> for Range<u32> {
    fn from(pg_range: PgRangeWrapper) -> Self {
        let start = match pg_range.0.start {
            Bound::Excluded(i) => (i + 1) as u32,
            Bound::Included(i) => i as u32,
            Bound::Unbounded => 0,
        };
        let end = match pg_range.0.end {
            Bound::Excluded(i) => i as u32,
            Bound::Included(i) => (i + 1) as u32,
            Bound::Unbounded => u32::MAX,
        };
        Range {
            start: start,
            end: end,
        }
    }
}

impl From<Range<u32>> for PgRangeWrapper {
    fn from(value: Range<u32>) -> Self {
        PgRangeWrapper(PgRange {
            start: Bound::Included(value.start as i64),
            end: Bound::Excluded(value.end as i64),
        })
    }
}

impl Repository for PgRepo {
    #[instrument(skip(self, payload))]
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
    ) -> Result<(), RepoError> {
        let start = Instant::now();
        let res = sqlx::query!(
            "INSERT INTO operations (topic_id, user_id, client_id, operation_offset, payload) 
            VALUES ($1, $2, $3, $4, $5)
            ON CONFLICT DO NOTHING",
            topic_id,
            user_id,
            client_id as i64,
            // this will wrap if the u64 overflows the i64
            // I don't think this will be an issue though because the
            // client id is created from a u32
            offset as i64,
            payload,
        )
        .execute(&self.pool)
        .await;
        let ret = match res {
            Ok(_) => Ok(()),
            Err(e @ sqlx::error::Error::InvalidArgument(_)) => Err(RepoError {
                kind: ErrorKind::SchemaMismatch,
                source: Box::new(e),
            }),
            Err(e) => Err(RepoError {
                kind: ErrorKind::FailedWrite,
                source: Box::new(e),
            }),
        };

        self.metrics_pg.db_client_operations_duration.record(
            start.elapsed().as_secs_f64(),
            &[
                KeyValue::new("db.system.name", "postgresql"),
                KeyValue::new("db.operation.name", "INSERT"),
                KeyValue::new("db.collection.name", "operations"),
                // TODO: this should not be a magic string, we should read this upon creating the repo
                KeyValue::new("db.namespace", "message_proxy"),
                KeyValue::new(
                    "db.query.summary",
                    "write the contents of an operation sync message",
                ),
                // TODO: extend this to read database returned error information on failure
                // TODO: would prefer to do this without making any heap allocations
                // KeyValue::new("error.type", "connection_timeout"),
                // KeyValue::new("db.response.status_code", ),
            ],
        );

        ret
    }
    #[instrument(skip(self, state_vector), fields(count_updates))]
    async fn read_operations_after(
        &self,
        state_vector: &[(u64, u32)],
        topic_id: uuid::Uuid,
    ) -> Result<Vec<Vec<u8>>, RepoError> {
        let start = Instant::now();
        // TODO: this may be an issue: we are casing unsigned int 32s to signed int 32s so
        // that they may be stored in postgres. This means that if we are doing numerical
        // operations on the operation_offset column inside postgres, then we may be operating
        // on negative signed integers that represent positive unsigned integers. This would
        // result in lost operation reads
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
                OR o.operation_offset >= version_vector.min_offset
            ) AND o.topic_id = $3",
            &state_vector
                .iter()
                .map(|(k, _)| *k as i64)
                .collect::<Vec<i64>>(),
            &state_vector
                .iter()
                .map(|(_, v)| *v as i64)
                .collect::<Vec<i64>>(),
            topic_id,
        )
        .fetch_all(&self.pool)
        .await;
        let ret = match operations {
            Ok(operations) => {
                tracing::Span::current().record("count_updates", operations.len());
                Ok(operations.into_iter().map(|op| op.payload).collect())
            }
            Err(e @ sqlx::error::Error::InvalidArgument(_)) => Err(RepoError {
                kind: ErrorKind::SchemaMismatch,
                source: Box::new(e),
            }),
            Err(e) => Err(RepoError {
                kind: ErrorKind::FailedRead,
                source: Box::new(e),
            }),
        };

        self.metrics_pg.db_client_operations_duration.record(
            start.elapsed().as_secs_f64(),
            &[
                KeyValue::new("db.system.name", "postgresql"),
                KeyValue::new("db.operation.name", "SELECT"),
                KeyValue::new("db.collection.name", "operations"),
                // TODO: this should not be a magic string, we should read this upon creating the repo
                KeyValue::new("db.namespace", "message_proxy"),
                KeyValue::new(
                    "db.query.summary",
                    "read the operations with a happens after relationship given a state vector",
                ),
                // TODO: extend this to read database returned error information on failure
                // TODO: would prefer to do this without making any heap allocations
                // KeyValue::new("error.type", "connection_timeout"),
                // KeyValue::new("db.response.status_code", ),
            ],
        );

        ret
    }
    #[instrument(skip(self), fields(last_received_offset))]
    async fn read_last_received_offset(
        &self,
        topic_id: uuid::Uuid,
        client_id: u64,
    ) -> Result<Option<u32>, RepoError> {
        let start = Instant::now();
        let result = sqlx::query!(
            "SELECT MAX(operation_offset) AS max_offset FROM operations
            WHERE topic_id =$1 AND client_id =$2",
            topic_id,
            client_id as i64,
        )
        .fetch_one(&self.pool)
        .await;
        let ret = match result {
            Ok(record) => {
                tracing::Span::current().record("last_received_offset", record.max_offset);
                Ok(record.max_offset.map(|o| o as u32))
            }
            Err(e @ sqlx::error::Error::InvalidArgument(_)) => Err(RepoError {
                kind: ErrorKind::SchemaMismatch,
                source: Box::new(e),
            }),
            Err(e) => Err(RepoError {
                kind: ErrorKind::FailedWrite,
                source: Box::new(e),
            }),
        };

        self.metrics_pg.db_client_operations_duration.record(
            start.elapsed().as_secs_f64(),
            &[
                KeyValue::new("db.system.name", "postgresql"),
                KeyValue::new("db.operation.name", "SELECT"),
                KeyValue::new("db.collection.name", "operations"),
                // TODO: this should not be a magic string, we should read this upon creating the repo
                KeyValue::new("db.namespace", "message_proxy"),
                KeyValue::new(
                    "db.query.summary",
                    "read the offset of the last received operation given a client and topic",
                ),
                // TODO: extend this to read database returned error information on failure
                // TODO: would prefer to do this without making any heap allocations
                // KeyValue::new("error.type", "connection_timeout"),
                // KeyValue::new("db.response.status_code", ),
            ],
        );

        ret
    }
    async fn read_doc_deletion_set(
        &self,
        topic_id: uuid::Uuid,
    ) -> Result<HashMap<u64, super::ClientDeletionSet>, RepoError> {
        let start = Instant::now();
        let result = sqlx::query!(
            r#"WITH doc_deletion_set AS(
                SELECT client_id, unnest(delete_set) as deletion_range
                FROM deletions
                WHERE topic_id = $1
            )
            SELECT client_id, array_agg(deletion_range) as "client_deletion_set!"
            FROM doc_deletion_set
            GROUP BY client_id"#,
            topic_id
        )
        .fetch_all(&self.pool)
        .await;
        let ret = match result {
            Ok(records) => {
                let deletions_by_client = records
                    .iter()
                    .map(|r| {
                        let ranges: Vec<Range<u32>> = r
                            .client_deletion_set
                            .iter()
                            .cloned()
                            .map(PgRangeWrapper)
                            .map(|w| w.into())
                            .collect();
                        (r.client_id as u64, ranges)
                    })
                    .collect();
                Ok(deletions_by_client)
            }
            Err(e @ sqlx::Error::InvalidArgument(_)) => Err(RepoError {
                kind: ErrorKind::SchemaMismatch,
                source: Box::new(e),
            }),
            Err(e) => Err(RepoError {
                kind: ErrorKind::FailedRead,
                source: Box::new(e),
            }),
        };
        self.metrics_pg.db_client_operations_duration.record(
            start.elapsed().as_secs_f64(),
            &[
                KeyValue::new("db.system.name", "postgres"),
                KeyValue::new("db.operation.name", "SELECT"),
                KeyValue::new("db.collection.name", "deletions"),
                KeyValue::new("db.namespace", "message_proxy"),
                KeyValue::new("db.query.name", "read the deletion set for a document"),
            ],
        );
        ret
    }
    /// Conditionally inserts the delete set or updates the existing delete set multirange
    /// if the new delete set has novel deletes. Returns a bool representing if we actually
    /// made the insert or update, meaning that this delete set is novel
    async fn write_deletion_set_if_novel(
        &self,
        topic_id: uuid::Uuid,
        user_id: uuid::Uuid,
        deletion_set: &HashMap<u64,super::ClientDeletionSet>,
    ) -> Result<bool, RepoError> {
        /* 
        goal: we want to create rows of (client_id, int8multirange)
        tools:
            - unnest will destructure an array into a bag of rows 
            - multirange converts a range into a multirange with just one range in it
            - range_agg will merge many multiranges
        Approach:
            - flatten the mapping (client_id, Vec<Range<u32>>) into the array Vec<(client_id, Range<u32>)>
              where the client_id is associated with each Range<u32> in it's corresponding Vec of ranges
            - merge the ranges into a multirange at the db using range_agg(multirange())
        */
        let start = Instant::now();
        let mut flat_client_ids: Vec<i64> = Vec::new();
        let mut flat_pg_ranges: Vec<PgRangeWrapper> = Vec::new();

        for (&client_id, client_deletion_set) in deletion_set.iter() {
            for range in client_deletion_set.iter().cloned() {
                flat_client_ids.push(client_id as i64);
                flat_pg_ranges.push(range.into());
            }
        }
        let result = sqlx::query!(
            // reference this: https://github.com/transact-rs/sqlx/issues/294
            // use unnest to destructure arrays of values for each column into
            // records in a cte
            // upserts is a cte of TRUE records for each successful insertion
            // if upserts is empty that means that either the delete set was 
            // empty or all the elements in the delete set were not novel
            // relative to the values that are in the database
            r#"WITH merged AS (
                SELECT id, range_agg(multirange(range)) AS merged_multirange
                FROM unnest($1::bigint[], $2::int8range[]) AS ranges(id, range)
                GROUP BY id
            ), upserts AS (
                INSERT INTO deletions (topic_id, user_id, client_id, delete_set)
                SELECT $3, $4, id, merged_multirange
                FROM merged
                ON CONFLICT (topic_id, client_id) DO UPDATE
                    SET delete_set = deletions.delete_set + excluded.delete_set
                    WHERE NOT deletions.delete_set @> excluded.delete_set
                RETURNING TRUE
            )
            SELECT EXISTS (SELECT 1 FROM upserts) AS "performed_insert!""#,
            &flat_client_ids,
            // this helps the compiler know which trait to use to serialize flat_pg_ranges
            &flat_pg_ranges as &[PgRangeWrapper],
            topic_id,
            user_id,
        )
        .fetch_one(&self.pool)
        .await;
        let ret = match result {
            Ok(r) => Ok(r.performed_insert),
            Err(e @ sqlx::Error::InvalidArgument(_)) => Err(RepoError {
                kind: ErrorKind::SchemaMismatch,
                source: Box::new(e),
            }),
            Err(e) => Err(RepoError {
                kind: ErrorKind::FailedWrite,
                source: Box::new(e),
            }),
        };
        self.metrics_pg.db_client_operations_duration.record(
            start.elapsed().as_secs_f64(),
            &[
                KeyValue::new("db.system.name", "postgres"),
                KeyValue::new("db.operation.name", "INSERT"),
                KeyValue::new("db.collection.name", "deletions"),
                KeyValue::new("db.namespace", "message_proxy"),
                KeyValue::new(
                    "db.query.name",
                    "write deletion set for a client_id if it is novel",
                ),
            ],
        );
        ret
    }
}

#[cfg(test)]
mod tests {
    use crate::repository::{self, RepoError, Repository, postgres::PgRepo};
    use sqlx::{Pool, Postgres};
    use sqlx_tracing::Pool as TracedPool;
    use std::{
        ops::Range, 
        vec,
        collections::HashMap,
    };
    use uuid::Uuid;

    #[sqlx::test]
    async fn test_write_read_all_operations(pool: Pool<Postgres>) -> Result<(), RepoError> {
        let (topic_id, user_id, client_id) = (Uuid::nil(), Uuid::nil(), 1 as u64);

        let repo = PgRepo::new(TracedPool::from(pool));
        let operation_1: Vec<u8> = vec![1, 2, 3];
        let operation_2: Vec<u8> = vec![4, 5, 6];
        // write two operations to the database
        let _ = repo
            .write_operation(topic_id, user_id, client_id, 0, &operation_1)
            .await?;
        let _ = repo
            .write_operation(topic_id, user_id, client_id, 1, &operation_2)
            .await?;
        // read those operations
        let state_vector: Vec<(u64, u32)> = vec![];
        let operations = repo.read_operations_after(&state_vector, topic_id).await?;
        assert_eq!(operations.len(), 2);
        assert_eq!(operations[0], operation_1);
        assert_eq!(operations[1], operation_2);
        Ok(())
    }
    #[sqlx::test]
    async fn test_write_read_operations_after_offset(
        pool: Pool<Postgres>,
    ) -> Result<(), RepoError> {
        let (topic_id, user_id, client_id) = (Uuid::nil(), Uuid::nil(), 1 as u64);

        let repo = PgRepo::new(TracedPool::from(pool));
        let operation_1: Vec<u8> = vec![1, 2, 3];
        let operation_2: Vec<u8> = vec![4, 5, 6];
        // write two operations to the database
        let _ = repo
            .write_operation(topic_id, user_id, client_id, 0, &operation_1)
            .await?;
        let _ = repo
            .write_operation(topic_id, user_id, client_id, 1, &operation_2)
            .await?;

        let state_vector: Vec<(u64, u32)> = vec![(client_id, 1)];
        let operations = repo.read_operations_after(&state_vector, topic_id).await?;
        assert_eq!(operations.len(), 1);
        assert_eq!(operations[0], operation_2);
        Ok(())
    }
    #[sqlx::test]
    async fn test_write_read_operations_multi_client(
        pool: Pool<Postgres>,
    ) -> Result<(), RepoError> {
        let repo = PgRepo::new(TracedPool::from(pool));
        // the goal of this test is to validate that the version vector logic works when
        // we have both missed operations from clients that we have seen operations for, and
        // missed operations from clients that we have not seen operations for
        let (topic_id, user_id) = (Uuid::nil(), Uuid::nil());
        let client_1: u64 = 1;
        let client_2: u64 = 2;
        let operations = vec![
            (client_1, 0u32, vec![1u8, 2, 3]),
            (client_1, 1u32, vec![4u8, 5, 6]),
            (client_2, 0u32, vec![7u8, 8, 9]),
        ];
        for elem in operations {
            repo.write_operation(topic_id, user_id, elem.0, elem.1, &elem.2)
                .await?;
        }

        let version_vector = vec![(1u64, 1u32)];
        let read = repo
            .read_operations_after(&version_vector, topic_id)
            .await?;
        assert_eq!(read.len(), 2);
        assert!(read.contains(&vec![4u8, 5, 6]));
        assert!(read.contains(&vec![7u8, 8, 9]));

        Ok(())
    }
    #[sqlx::test]
    async fn test_write_read_last_offset(pool: Pool<Postgres>) -> Result<(), RepoError> {
        let (topic_id, user_id, client_id) = (Uuid::nil(), Uuid::nil(), 1 as u64);

        let repo = PgRepo::new(TracedPool::from(pool));
        let operation_1: Vec<u8> = vec![1, 2, 3];
        // write two operations to the database
        let _ = repo
            .write_operation(topic_id, user_id, client_id, 0, &operation_1)
            .await?;

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
        let (topic_id_a, topic_id_b, user_id, client_id) =
            (Uuid::new_v4(), Uuid::new_v4(), Uuid::nil(), 1 as u64);
        let operation_1: Vec<u8> = vec![1, 2, 3];
        // write an operation at offset one on the first topic
        repo.write_operation(topic_id_a, user_id, client_id, 0, &operation_1)
            .await?;
        // verify that the operation offset can be read on the first topic
        let result = repo
            .read_last_received_offset(topic_id_a, client_id)
            .await?;
        assert_eq!(result, Some(0));
        // try to read that operation on the second topic
        let result = repo
            .read_last_received_offset(topic_id_b, client_id)
            .await?;
        // verify that the read operation is not found
        assert_eq!(result, None);

        Ok(())
    }
    #[sqlx::test]
    async fn test_write_read_delete_set(pool: Pool<Postgres>) -> Result<(), RepoError> {
        // create a repo object from the pool
        let repo = PgRepo::new(TracedPool::from(pool));
        // create a deletion set to be inserted
        let (topic_id, user_id, client_id) = (Uuid::new_v4(), Uuid::new_v4(), 1 as u64);
        let delete_set: HashMap<u64, repository::ClientDeletionSet> = HashMap::from([
            (client_id, vec![Range { start: 0, end: 3 }, Range { start: 7, end: 9 }]),
        ]);
        // insert the deletion set, asserting that the deletion set actually was inserted
        let was_inserted = repo
            .write_deletion_set_if_novel(topic_id, user_id, &delete_set)
            .await?;
        assert!(
            was_inserted,
            "the delete set was not deemed novel or inserted"
        );
        // read the inserted deletion set, asserting that it is equivalent to the deletion
        // set that was inserted
        let result = repo.read_doc_deletion_set(topic_id).await?;
        match result.get(&client_id) {
            None => panic!("the deletion set for the expected client_id was not returned"),
            Some(ret_deletion_set) => {
                // order does not matter for the ranges in the deletion set, we are only
                // concerned with the fact that each range is present and there are no
                // extra ranges.
                assert_eq!(
                    delete_set.get(&client_id).unwrap().len(),
                    ret_deletion_set.len(),
                    "the returned deletion set is a different size than the written deletion set",
                );
                let has_all_deletes = delete_set
                    .iter()
                    .all(|(_, ranges)| {
                        ranges.iter().all(|r| ret_deletion_set.contains(r))
                    });
                assert!(
                    has_all_deletes,
                    "a delete from delete set {:?} was missing from returned delete set {:?}",
                    delete_set, ret_deletion_set,
                );
            }
        };
        Ok(())
    }
    #[sqlx::test]
    async fn test_write_redundant_delete_set(pool: Pool<Postgres>) -> Result<(), RepoError> {
        let repo = PgRepo::new(TracedPool::from(pool));
        let (topic_id, user_id, client_id) = (Uuid::new_v4(), Uuid::new_v4(), 1 as u64);
        let first_delete = HashMap::from([
            (client_id, vec![Range { start: 0, end: 4 }, Range { start: 6, end: 18 }])
        ]);
        let second_delete = HashMap::from([
            (client_id, vec![Range { start: 1, end: 4 }, Range { start: 6, end: 9 }])
        ]);
        // write a delete set to the table
        let first_write = repo
            .write_deletion_set_if_novel(topic_id, user_id, &first_delete)
            .await?;
        assert!(
            first_write,
            "first delete set was deemed not novel and not written to the table"
        );
        // attempt to write a second delete set to the table that is a subset of the first delete set
        let second_write = repo
            .write_deletion_set_if_novel(topic_id, user_id, &second_delete)
            .await?;
        assert!(
            !second_write,
            "expected second delete set to be found not novel and not written to the table, found it was novel",
        );
        // read the delete set for that topic_id, client_id combo
        let delete_set = repo.read_doc_deletion_set(topic_id).await?;
        let client_delete_set = delete_set.get(&client_id);
        assert!(
            client_delete_set != None,
            "expected to find deletions for this client, found None"
        );
        if let Some(ds) = client_delete_set {
            assert!(
                // rust equivalence will implicitly dereference here so that we can use the 
                // partial equals trait
                ds == first_delete.get(&client_id).expect("client_id missing from delete set"),
                "expected the read client delete set to match the first delete found that it did not match",
            )
        }
        Ok(())
    }
    #[sqlx::test]
    async fn test_write_overlapping_delete_set(pool: Pool<Postgres>) -> Result<(), RepoError> {
        let repo = PgRepo::new(TracedPool::from(pool));
        let (topic_id, user_id, client_id) = (Uuid::new_v4(), Uuid::new_v4(), 1 as u64);
        let first_delete: HashMap<u64,Vec<Range<u32>>> = HashMap::from([
            (client_id, vec![Range { start: 0, end: 3 }, Range { start: 5, end: 7 }])
        ]);
        let second_delete: HashMap<u64, Vec<Range<u32>>> = HashMap::from([
            (client_id, vec![Range { start: 3, end: 5 }])
        ]);
        // write a delete set to the table
        let first_write = repo
            .write_deletion_set_if_novel(topic_id, user_id, &first_delete)
            .await?;
        assert!(
            first_write,
            "expected: the first write to be inserted, found: that it was not"
        );
        // write a second delete set to the table with some overlapping values with the first
        // delete set
        let second_write = repo
            .write_deletion_set_if_novel(topic_id, user_id, &second_delete)
            .await?;
        // validate that the result of the second write indicated that the second write went through
        assert!(
            second_write,
            "expected: the second write to be inserted, found: that it was not"
        );
        // read the delete set for this topic_id client id combo
        let delete_set = repo.read_doc_deletion_set(topic_id).await?;
        // validate that the returned delete set is the union of the two written delete sets
        let expected_delete_set: Vec<Range<u32>> = vec![Range { start: 0, end: 7 }];
        let per_client_delete = delete_set.get(&client_id);
        assert_eq!(
            *per_client_delete.expect("no client delete set found after insertion"),
            expected_delete_set,
            "retrieved delete set for client_id: {} was not the same as the expected delete set",
            client_id,
        );
        Ok(())
    }
    #[sqlx::test]
    async fn test_independent_delete_set_upserts(pool: Pool<Postgres>) -> Result<(), RepoError> {
        let repo = PgRepo::new(TracedPool::from(pool));
        let (topic_id, user_id, client_id_a, client_id_b) =
            (Uuid::new_v4(), Uuid::new_v4(), 1 as u64, 2 as u64);
        let delete_set_a: HashMap<u64, Vec<Range<u32>>> = HashMap::from([
            (client_id_a, vec![Range { start: 2, end: 4 }])
        ]);
        let delete_set_b: HashMap<u64, Vec<Range<u32>>> = HashMap::from([
            (client_id_b, vec![Range { start: 3, end: 9 }])
        ]);
        // insert a delete set for one client
        let write_a = repo
            .write_deletion_set_if_novel(topic_id, user_id, &delete_set_a)
            .await?;
        assert!(
            write_a,
            "expected the write delete set to go through, found that it was deemed a duplicate"
        );
        // insert a delete set for a different client but the same topic id
        let write_b = repo
            .write_deletion_set_if_novel(topic_id, user_id, &delete_set_b)
            .await?;
        assert!(
            write_b,
            "expected the write delete set to go through, found that it was deemed a duplicate"
        );
        // read the delete set for that topic_id
        let delete_set_topic = repo.read_doc_deletion_set(topic_id).await?;
        // verify that the two insertions are independent
        assert_eq!(
            delete_set_topic
                .get(&client_id_a)
                .expect("delete set for client a was not read from the database"),
            delete_set_a
                .get(&client_id_a)
                .expect("delete set for client a not read from hard coded input"),
            "expected the delete set for client a written to the db to match the delete set read from the db",
        );
        assert_eq!(
            delete_set_topic
                .get(&client_id_b)
                .expect("delete set for client b was not read from the database"),
            delete_set_b
                .get(&client_id_b)
                .expect("delete set for client b not found in hard coded input"),
            "expected the delete set for client b written to the db to match the delete set read from the db"
        );
        Ok(())
    }
    // #[sqlx::test]
    // async fn test_range_conversion_boundary_behavior(pool: Pool<Postgres>) -> Result<(), RepoError> {
    //     // we currently have no way of writing ranges other than [)
    //     //  - this is inclusive lower bound and exclusive upper bound
    //     // until we have a way to write ranges with different boundary semantics I
    //     // don't think it is necessary to test the boundary behavior
    //     // merging ranges is already tested here test_write_overlapping_delete_set
    //     Ok(())
    // }
    // #[sqlx::test]
    // async fn test_write_invalid_range(pool: Pool<Postgres>) -> Result<(), RepoError> {
    //     // write some bad data and validate that the returned error type is consistent with what
    //     // we expect
    //     Ok(())
    // }
}

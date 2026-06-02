## Functional requirements:
- scale to many concurrent editors of a document

## Technical requirement:
- at 100 concurrent editors writing 200 operations per minute to a document, we are seeing 20,000 writes per minute or 333 writes per second
    - each of these writes is very small, a handful of bytes
    - nonetheless I am seeing very high latency in the websocket read path 
        - this reads from the websocket and writes the message to postgres
- [ ] identify the source of the issue
    - [x] add postgres metrics to clickstack
        - the clickhouse/clickstack-otel-collector does not natively have the `postgresqlreceiver` extension installed
            - https://github.com/hyperdxio/hyperdx/blob/main/packages/otel-collector/builder-config.yaml
        - [x] create a separate otel collector that scrapes postgres for metrics and pushes those metrics to the clickstack otel collector otlp endpoint
    - [ ] visualize postgres stats
    - [x] add sqlx library instrumentation
        - https://docs.rs/sqlx-tracing/latest/sqlx_tracing/
        - turns out this is just tracing, not metrics
    - [ ] add axum otel library instrumentation
        - https://crates.io/crates/axum-tracing-opentelemetry
    - [x] add manual metrics instrumentation to ws portion
    - [x] add manual metrics instrumentation to the repo portion
    - [x] add tracing for the hot path
        - [x] add tracing exporter and tracer provider using the sdk
        - [x] add the tracing_opentelemetry layer that allows us to intercept traces created by the tracing library and route them to the otel backend
- [ ] if postgres write through put + coordination and metadata overhead associated with many writes is the reason that we have high write latency, fix the problem by batching at the task level and the instance level
    - [ ] optimistically read operation messages from the websocket until there are no more operation messages
    - [ ] update the repo abstraction to support submitting writes as part of a larger batch split between async tasks
        - use the "send the sender" concurrency approach
        - reader task submits the operation to be written along with a oneshot receiver to the repo
        - internally the repo uses a mpsc channel to centralize writes from many task to one task that performs batch writes of operation messages
        - that task atomically writes large batches of operations then sends on the oneshot sender to each waiting task to indicate that the write succeeded
        - the batch write function waits for a result from the batch write task then returns the result to the client
    - [ ] broadcast messages to other tasks using the broadcast channel upon persisting operations
    - [ ] retry the write on this thread without using the batch upon failure 

## Resources:
- configuring the `postgresqlreceiver` receiver
    - https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/receiver/postgresqlreceiver
    - -- In postgresql.conf -- shared_preload_libraries = 'pg_stat_statements'
- otel custom metrics instrumentation example:
    - https://github.com/open-telemetry/opentelemetry-rust/blob/main/opentelemetry-otlp/examples/basic-otlp/src/main.rs
    - https://github.com/open-telemetry/opentelemetry-rust/blob/main/examples/metrics-advanced/src/main.rs
- alternative metrics instrumentation tool:
    - https://docs.rs/tracing-opentelemetry/latest/tracing_opentelemetry/struct.MetricsLayer.html
    - https://crates.io/crates/init-tracing-opentelemetry#Metrics
    - ultimately decided not to go with this approach because it seems less flexible and complete than the otel sdk
- semantic conventions for database client metrics instrumentation
    - https://opentelemetry.io/docs/specs/semconv/db/database-metrics/#metric-dbclientoperationduration
- clickstack postgres monitoring instructions:
    - https://clickhouse.com/docs/use-cases/observability/clickstack/integrations/postgresql-metrics#dashboards
## Functional Spec:
- measure the operations of the message proxy service 
    - on average how long until we get to the hot path
    - how long does it take to receive a client sync step one message
        - wall clock time
    - after we have received a client sync step one message, how long does it take for us to produce a server sync step two message and transition to the writer hot path
        - writer hot path is unblocked by sending server sync step two message
        - measure both wall clock time and active time
        - it is more important to reach the writer hot path quickly because if the writer does not start reading from the broadcast channel, it can lag causing dropped messages
        - this is the length of our writer handshake process
        - measure the time from when we receive the client sync step one message at the reader task to when we have sent the server sync step two message at the writer task
    - how long does it take to produce and send a server sync step one message
        - this allows the client to send a client sync step two message, which unblocks the reader task hot path
    - after we have received a client sync step two message, how long does it take for us to persist that message, broadcast that update, and enter the reader hot path
        - measure both wall clock time and active time
        - reader hot path is unblocked by client sync step two message
        - this measure is not as important because it does not block us from reaching the writer hot path, only the reader hot path
        - reaching the reader hot path can be done slightly slower because the websocket reader guarantees that dropped messages will be retransmitted with back-pressure
        - slow websocket readers slow down exactly one writer (the client) instead of potentially many writers
    - how long do our database reads take
    - how large are the updates that clients are sending
    - how long does it take to read, persist, and broadcast one message
        - this is the length of the reader hot path loop
    - when a reader receives a operation from a client for document A, how long does it take for all writer tasks associated with that document id to receive that update operation
        - think about this in the context of one server or many servers
        - client to client latency
    - how frequently are we lagging
    - how long does it take to read an update message from the websocket, persist it, and write it to the broadcast channel
        - need to know both the wall clock time and active time
        - this is the length of the readers hot path loop
    - how long does it take to read a message from the broadcast channel and send it to the client
        - need to know both wall clock time and active time
        - this is the length of the writer hot path loop
- measure the operations of the message proxy service with the client
    - how long does it take to make one round trip:
        - client A -> message_proxy service -> client B (receives message and updates local state)

## Technical Spec:
- introduce ClickStack observability
- add opentelemetry tracing to the message proxy service
- add opentelemetry tracing to the tui client
- use the "log lines" pattern from stripe so that we can dynamically query logs to derive metrics without needing to know the metrics ahead of time


## Tasks:
- [ ] manual logging instrumentation
    - [x] transition from client sync step one to server sync step two and writer hot path
    - [x] transition from client sync step two message to reader hot path
    - [x] read hot path loop
    - [x] write hot path loop
- [ ] aggregations over canonical log lines
    - [ ] transition from client sync step one to server sync step two 
    - [ ] transition from client sync step two message to writer hot path
    - [ ] read hot path loop
    - [ ] write hot path loop
- [ ] add sqlx library instrumentation
    - https://docs.rs/sqlx-tracing/latest/sqlx_tracing/
- [ ] add axum otel library instrumentation
    - https://crates.io/crates/axum-tracing-opentelemetry
- [ ] add websocket message canonical-log-line for reader and writer tasks
- [ ] add tail sampling 

## Resources (observability platform):
- log lines:
    - https://stripe.com/blog/canonical-log-lines
        - at the end of processing a request emit one log line that includes many of the requests characteristics
            - number of memory allocations 
            - time spent on database queries
            - latency
            - use request level middleware to implement this 
            - log like protobuf:
                - https://github.com/stripe/veneur/tree/master/ssf
        - structured logs allow developers to tag logs with contextual data in a way that is both human readable and machine parseable 
        - log lines allow you to write creative and flexible queries
        - write logs to an olap database that supports tiered storage
    - https://brandur.org/nanoglyphs/025-logs
        - this is a good collection of software engineering best practices in general
        - add the http link to the trace in the log line so that you can easily jump from the log line to the trace
            - this is so brilliant because it is such a simple solution to what is also a simple problem
            - I don't have to fight with the otel collector config and the grafana temp config. This can be moved to the application level because it is easy
        - log in json but configure your log visualizer to show just the "message" field of the log output
- observability 2.0
    - https://www.honeycomb.io/blog/time-to-version-observability-signs-point-to-yes
        - aggregating at read time means that no context is lost before you write your query
        - metrics throw away valuable data because they don't support high cardinality
            - data is made valuable by context
        - if you can specify metrics at query time then you can use metrics as an unbounded exploration tool
            - debugging the system is like iteratively forming and validating hypotheses
    - http://honeycomb.io/blog/one-key-difference-observability1dot0-2dot0
        - one source of truth vs many sources of truth
- what is wrong with the three pillars:
    - https://softwareengineeringdaily.com/2021/02/04/debunking-the-three-pillars-of-observability-myth/
    - the work of making sense of the traces, logs, and metrics is left as an exercise to the reader
    - planet scale observability tools are intrinsically feature poor
    - pre aggregated metrics are a monitoring tool, not an observability tool
    - they only sometimes allow you to answer novel questions about your running system without modifying it
        - they are poor at solving some unknown-unknowns
    - tail sampling is important
- clickstack:
    - https://clickhouse.com/blog/clickstack-a-high-performance-oss-observability-stack-on-clickhouse
        - mostly annoying marketing material
        - clickhouse cloud separates query execution and data ingestion compute
        - click house has support for semi structured data
            - what does this mean exactly? Can I assign multiple types of values to a key in my log line
            - how does this compare to apache doris?
    - https://clickhouse.com/docs/use-cases/observability/clickstack/getting-started?loc=blog-o11y-global-cta&utm_source=clickhouse&utm_medium=web&utm_campaign=blog
        - docs
    - https://clickhouse.com/blog/the-state-of-sql-based-observability
        - wide events on clickstack
    - https://clickhouse.com/blog/evolution-of-sql-based-observability-with-clickhouse
        - more wide events on clickstack
- Json support for hyperdx is deprecated
- Key insights:
    - metrics data that is calculated on the server has no visibility into the context required to calculate aggregations over processes that span multiple servers
        - this can be overcome by including information like start time etc in the message that is sent from one server to another
        - ex: if an event is produced on one machine, how long until all of that machines peers finish processing that event
        - can spans be created that span multiple machines?

## Resources (observability instrumentation):


## Cleanup:
- add this:
    `cargo clippy -- -D warnings -D clippy::pedantic`

## Testing:

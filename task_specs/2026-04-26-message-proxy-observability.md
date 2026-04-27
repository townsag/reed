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
- [ ] this task is blocked by the containerize message proxy task

## Resources:
- log lines:
    - https://stripe.com/blog/canonical-log-lines
- observability 2.0
    - https://www.honeycomb.io/blog/time-to-version-observability-signs-point-to-yes
    - http://honeycomb.io/blog/one-key-difference-observability1dot0-2dot0

## Cleanup:
- add this:
    `cargo clippy -- -D warnings -D clippy::pedantic`

    
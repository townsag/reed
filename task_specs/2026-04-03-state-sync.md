## Functional Spec:
- clients connecting to documents that have existing edits should receive those edits before they can start editing the documents
- clients that experience faulty network connections should receive missed messages
- clients that are slow readers should receive missed updates

## Technical spec:
- the message proxy server makes websocket connections to many clients concurrently
- websocket messages can be dropped at the application level
    - operation messages sent from a client to the server are assumed to be received by the server and persisted to the database before they can be proxied to other connected clients
    - the mpmc channel that is used to proxy messages between connections within a message proxy instance uses ring buffer semantics:
        - if a very fast websocket reader broadcasts many websocket messages before all the clients are able to process them, those messages will be dropped
- for these reasons we need to implement an application level protocol for recovering missing operation messages
- [ ] state sync on lagged writer:
    - [ ] Server side changes:
        - [ ] keep track of the offset of the last update acknowledged by the client and the offset of the most recent in-flight message
            <!-- - keep track of the last update acknowledge when in the message recovery state  -->
                - ^for the scope of this task assume that the websocket connection will not lose messages at all even though it is possible to lose messages at the os buffer level
            - keep track of the most recent in-flight message state vector when in the hot path state
        <!-- - [ ] keep track of the offset of the last update sent by the client
            - [ ] if we receive an update that is out of order, reject the message and send a message to the client containing the last offset for which we have seen an update -->
            - ^for the scope of this task, assume that the websocket connection will not quietly lose messages
        - [ ] maintain two states of the system
            - hot path and message recovery
            - for the scope of this task we only need to maintain the message recovery state for the write task because we are assuming that the client will not write any messages out of order or lose messages
        - [ ] detect situations in which we should move from one state to the other
            - hot path -> recovery:
                <!-- - [ ] receive a state transfer request message from the client
                    - this message would include a 
                    - not sure how I feel about this request response pattern -->
                - [ ] detect that this task has lagged when reading from the broadcast queue
                - [ ] detect that a message that is read from the broadcast queue has an offset that does not immediately follow the offset of the most recent in-flight message
            - recovery -> hot path:
                - [ ] after successfully sending a state transfer response message
        - [ ] when the system is in the recovery state, use the recovery logic to send lost messages from the client
            - receive the clients state vector
            - read all the update messages from the database that have a state vector following the state vector sent by the client
            - merge those updates into a single update
            - send the merged update to the client
            - transition from message recovery to hot path
                - use the type state pattern to encode this transition
        <!-- - [ ] when the system is in the hot path, periodically tell the client what the last offset we have received is -->
        - when in the hot path state, continue to proxy messages as usual
        - use biased polling to change the order in which the writer receives events
    - [ ] client side changes:
        - [ ] detect situations in which we should move from one state to the other
            - [ ] decode the update message and identify if the state vector in the update message is
        - [ ] still apply update messages even if we are in the message recovery phase
            - these messages will be cached at the YDoc level and then applied when the prerequisite messages have been received
- websocket messages can be dropped at the network level:
    - dropped tcp connections
    - messages sent during tcp handshake
    - buffer overflows in the nic or the os or the runtime
- [ ] state sync on OS buffer overflow, resulting in missed ws message
    - this will manifest as an error when receiving from the websocket stream
    - this is outside of the scope of this task
- [x] state sync on handshake:
    - when a new websocket connection is created with a client, that client may have locally applied operations that are not synced with the server. Furthermore, remote clients editing the same document might have operations that are not synced with the new client.
    - for those reasons we need a state sync handshake when creating a new websocket connection
    - insight:
        - the syncing all updates from remote clients to the client is independent from syncing all updates from the client to the server
        - we can treat these two tasks as independent
    - [x] ensure that the client has all the updates from the remote clients
        - states:
            - server writer: (waiting for handshake) --client version vector--> (hot path) + server bulk update
        - [x] the client sends a message with the version vector of updates that it has received
        - [x] the server sends a bulk update message with all the updates from remote clients with a happens after relationship relative to the version vector
        - server switches to hot path
        - we could wait for an ack from the client here, I am still thinking about that
            - we do not need an ack here because a disconnect or dropped message will both result in a new websocket connection being created and a new handshake
    - [x] ensure that the server has all the local updates from the client
        - states:
            - server reader: (waiting for handshake) + current version --client bulk update--> (hot path)
                - client bulk update can be empty if we are up to date
            - server reader: (waiting for init) + current version --client version vector--> (message recovery) + message sync request --bulk update message--> (hot path) + sync done
            - or:            (waiting for init) + current version --client version vector--> (hot path) + sync done
        - the server sends the offset of the most recent operation that it has received from this client
        - the client sends a bulk update of all the operations with a happens after relationship relative to the last seen offset by the server
            - the client bulk update can be empty
        - client switches to hot path
- [x] database changes:
    - [x] add the idea of migrations to the message proxy service
        - this allows us to perform these migrations as part of integration tests
        - https://docs.rs/sqlx/latest/sqlx/migrate/trait.MigrationSource.html
        - you can run migrations using this pattern
        ```bash
        sqlx migrate info
        ```
        - you can populate the cache of sqlx table shapes using
        ```bash
        cargo sqlx prepare
        ```
        - then change the env variable that indicates we should use offline mode
    - tests are run in their own isolated database, that is why we use the migrations. This allows tests to use the database schema as well as be independent
- [x] cleanup:
    - add documentation for:
        - running dev database
        - running database migrations
        - setting the cache of schemas to enable local development without the dev database server
        - running the tui



- Resources:
    - y-protocol documentation and implementation
        - https://docs.rs/yrs/latest/yrs/sync/protocol/index.html
    - yrs StateVector documentation:
        - https://docs.rs/yrs/latest/yrs/struct.StateVector.html
    - y-sync documentation
        - helpful for using an already defined wire format for sync and update messages

- testing:
    - run the database and the websocket message proxy server:
    ```bash
    cd message_proxy
    docker compose -f docker-compose-sqlx.yml up -D
    cargo run --bin message_proxy
    ```
    - open a new terminal and run one instance of the tui
    ```bash
    cd message_proxy
    cargo run --bin tui -- localhost:3000 00000000-0000-0000-0000-000000000000 00000000-0000-0000-0000-000000000001 1 2> error2.log
    ```
    - make manual edits in this window
    - open a new terminal and run a second instance of the tui with the same topic_id but a different client_id
    ```bash
    cd message_proxy
    cargo run --bin tui -- localhost:3000 00000000-0000-0000-0000-000000000000 00000000-0000-0000-0000-000000000001 2 2> error2.log
    ```
    - observe that the edits made by the other client are visible
    - make manual edits in either window
    - observe that edits are synced between tui clients

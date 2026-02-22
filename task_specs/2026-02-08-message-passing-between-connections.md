## Description:
- at this point the message proxy is a single instance service that echos messages back to the client that sent them
    - there is no communication between clients because there is no communication between two websocket connections 
- when a message is sent to an instance of the message proxy service, that message should be proxied to all other clients that are connected to that instance of the message proxy service

## Technical Approach:
- add global state to the message proxy service and pass that to each handler invocation
    - this will allow us to pass messages between websocket connections 
    - https://docs.rs/axum/latest/axum/extract/ws/index.html#passing-data-andor-state-to-an-on_upgrade-callback
- split the message handler into a component that reads messages from the websocket connection and a second component that sends messages over the websocket connection
    - this will allow us to separate the tasks of listening for messages from a client and sending messages from another client so that they are not blocking each other
    - https://docs.rs/axum/latest/axum/extract/ws/index.html#read-and-write-concurrently
- create a broker which receives messages from websocket connections and broadcasts the messages to all other clients
    - use multiple producer single consumer pattern to send messages from all websocket connections to the broker
        - use tokio::sync::mpsc;
        - https://docs.rs/tokio/latest/tokio/sync/mpsc/fn.channel.html
        - https://tokio.rs/tokio/tutorial/channels
    - the broker maintains spsc channel connections with each sender task
    - when the broker receives a message over the channel it forwards that message to the relevant other connections
    - https://claude.ai/chat/8588c7eb-6b91-43bf-81cc-5bc162247cb9
- good example using axum websockets:
    - https://github.com/tokio-rs/axum/blob/main/examples/chat/src/main.rs
- specifics about .with_state in axum:
    - https://docs.rs/axum/latest/axum/struct.Router.html#method.with_state

## Notes:
- the semantics for accessing values declared outside of an async task are similar to the semantics of accessing values that are declared outside of a thread
    - the task must take ownership of the variable and the variable must move into the task
- tasks must implement the Send bound
    - this means that tasks must be movable between threads while they are suspended
    - state that is used after an await in the task is saved on the stack
- tasks only yield when you call await
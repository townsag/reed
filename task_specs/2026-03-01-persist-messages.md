## Description:
- the goal of this task is to introduce the database machinery into the rust portion of the project, not to create the mechanism by which lagging clients can read dropped messages
- I want to understand the database access code patterns in rust 
- just write new messages to the database

## Tasks:
- [x] add a user_id path parameter to the websocket server
    - [x] update the websocket connection path to include the user_id and parse the user_id path parameter
- [x] create a database connection configuration module that can be used to initialize the server
    - [x] read database connection information from the environment
- [ ] write a docker file for this server
    - this is left for a future PR
    - [ ] build stage
    - [ ] run stage
- [x] create a repository module
    - [x] create a repository trait that can be implemented
        - [x] domain level errors
        - [x] write methods 
    - [x] create a repository struct that exposes methods that the application can use to access persistent storage
        - consider using the golang dependency inversion idiom in which the application (websocket handlers) depends on the database access interface instead of on the concrete implementation of the struct
        - [x] expose a method called write message that writes a websocket message to the database 
            - should include topic_id, user_id, and content
- [x] make the repository available at the request level by the handlers
- [x] call the write message function in the read handler

## Resources:
- example of using sqlx and axum
    - https://github.com/launchbadge/sqlx/blob/main/examples/postgres/axum-social-with-tests/src/http/user.rs
- thoughtful discussion of sqlc vs sqlx
    - https://news.ycombinator.com/item?id=44715579
    - includes discussion of query parsing
- useful for later if I dont want to have a running database in the cicd pipeline
    - https://github.com/launchbadge/sqlx/blob/main/sqlx-cli/README.md#enable-building-in-offline-mode-with-query

## Manual Testing:
- run the postgres server and the application server
```bash
cd message_proxy
cargo build
./target/debug/message_proxy
```
```bash
cd message_proxy
docker compose -f docker-compose-sqlx.yml up
```
- connect to the application server in two different terminals
```bash
websocat ws://localhost:3000/ws/00000000-0000-0000-0000-000000000000/00000000-0000-0000-0000-000000000002
```
```bash
websocat ws://localhost:3000/ws/00000000-0000-0000-0000-000000000000/00000000-0000-0000-0000-000000000001
```
- write a number of messages from each client
    - note the ordering of the messages
- observe that the messages are being received by the other client
- verify that the messages are persisted
```bash
psql -h localhost -p 5432 -U admin -d postgres
SELECT * FROM messages;
```a
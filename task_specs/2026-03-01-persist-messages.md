## Description:
- the goal of this task is to introduce the database machinery into the rust portion of the project, not to create the mechanism by which lagging clients can read dropped messages
- I want to understand the database access code patterns in rust 
- just write new messages to the database

## Tasks:
- [ ] add a user_id query parameter to the websocket server
    - [ ] update the websocket connection path to include the user_id and parse the user_id query parameter
- [ ] create a database connection configuration module that can be used to initialize the server
    - [ ] read database connection information from the environment
- [ ] write a docker file for this server
    - [ ] build stage
    - [ ] run stage
- [ ] create a repository module
    - [x] create a repository trait that can be implemented
        - [x] domain level errors
        - [x] write methods 
    - [ ] create a repository struct that exposes methods that the application can use to access persistent storage
        - consider using the golang dependency inversion idiom in which the application (websocket handlers) depends on the database access interface instead of on the concrete implementation of the struct
        - [ ] expose a method called write message that writes a websocket message to the database 
            - should include topic_id, user_id, and content
- [ ] make the repository available at the request level by the handlers
- [ ] call the write message function in the read handler

## Resources:
- example of using sqlx and axum
    - https://github.com/launchbadge/sqlx/blob/main/examples/postgres/axum-social-with-tests/src/http/user.rs
- thoughtful discussion of sqlc vs sqlx
    - https://news.ycombinator.com/item?id=44715579
    - includes discussion of query parsing
- useful for later if I dont want to have a running database in the cicd pipeline
    - https://github.com/launchbadge/sqlx/blob/main/sqlx-cli/README.md#enable-building-in-offline-mode-with-query
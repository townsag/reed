## General:
- all of these commands assume that your current working directory is reed/message_proxy/

## Run the Dev database:
```bash
docker compose -f docker-compose-sqlx.yml up
```
## Run database migrations:
```bash
sqlx migrate run
```
## Check which migrations have been run already:
```bash
sqlx migrate init
```
## Set the cache of database schemas so that you can still use the sqlx query! macro offline:
```bash
cargo sqlx prepare
# also modify the .env file to include <SQLX_OFFLINE=true>
```
## Run the websocket server:
```bash
cargo run --bin message_proxy
```
## Run the tui
```bash
cargo run --bin tui -- <hostname> <topic_id> <user_id> <client_id> 2> error2.log
```
example:
```
cargo run --bin tui -- localhost:3000 00000000-0000-0000-0000-000000000000 00000000-0000-0000-0000-000000000001 2 2> error2.log
```
FROM rust:1.94.1-alpine3.23 AS builder
# copy in the cargo file so we know what the dependencies are
COPY Cargo.lock Cargo.toml /app/
# download and build the dependencies
WORKDIR /app
# TODO: this is a hack, see cargo chef
# Stub entrypoint so we can cache dependency compilation
RUN mkdir -p src && echo 'fn main() {}' > src/main.rs
RUN --mount=type=cache,target=/usr/local/cargo/registry \
    --mount=type=cache,target=/app/target \
    cargo build --release
# copy in the sqlx query type information files so that we can build 
# sqlx query! code in offline mode
COPY ./.sqlx /app/.sqlx
# write an env file with the offline mode flag turned on
RUN echo 'SQLX_OFFLINE=true' > .env
# copy in the source code
COPY ./src /app/src
# build the application using the build cache
RUN --mount=type=cache,target=/usr/local/cargo/registry \
    --mount=type=cache,target=/app/target \
<<EOF
  set -e
  # update timestamps to force a new build
  touch /app/src/main.rs
  cargo build --release
  # copy the compiled server binary out of the mounted cache
  cp /app/target/release/message_proxy /bin/message_proxy
EOF


FROM alpine:3.23 AS runner
COPY --from=builder /bin/message_proxy /message_proxy
CMD [ "/message_proxy" ]
## Functional Requirement:
- testing the message proxy service should be simple:
    - no requirement for understanding how to use cargo or sqlx
    - no dependency between when terminal windows are started
- enable other features that require network configuration:
    - pub / sub
    - observability

## Technical Requirement:
- [x] write a docker file for building the message proxy service 
- [x] write a docker file that performs the migrations in the message proxy directory
- [x] write a docker compose file for the message proxy service and its dependencies
    - [x] run the postgres database 
    - [x] run the sqlx migration
    - [x] run the message proxy service
- [x] use a volume so the database state can be inspected between runs

## Cleanup Tasks:
- [x] ensure that the message proxy service is using a different database namespace as the user service or document service
- [x] provide a clean way to clear the postgres instance volume 

## Resources:
- example docker file:
    - https://github.com/rust-lang/docker-rust/blob/master/Dockerfile-alpine.template
- discussion on multi stage builds and build cache:
    - https://gist.github.com/noelbundick/6922d26667616e2ba5c3aff59f0824cd
- hacky medium article
    - https://medium.com/@ams_132/dockerizing-a-rust-application-with-multi-stage-builds-31ac8a5ce7c7
- cargo chef, multi stage build tool:
    - https://fly.io/docs/rust/the-basics/cargo-chef/
    - https://github.com/LukeMathWalker/cargo-chef

## Numbers:
- the runner is significantly smaller than the builder:
REPOSITORY               TAG         IMAGE ID       CREATED          SIZE
message-proxy            builder     d208e06e5bf6   37 seconds ago   1.4GB
message-proxy            1           1a3f1a520559   16 minutes ago   19.9MB
- the runner compiles in 31 seconds when I don't change any of the dependencies
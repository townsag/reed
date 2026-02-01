FROM golang:1.24 AS builder
# copy in the dependencies list and download the dependencies
WORKDIR /app
COPY go.mod go.sum ./
# go mod download works without referencing the source code, this makes
# it easier to cache the result of downloading the dependencies as a 
# docker container layer because the .mod and .sum files change 
# infrequently
# We use go mod download here instead of go mod tidy because go mod tidy 
# would require the source code to also be copied into the docker image 
# before downloading the dependencies. This would mean that the cached
# layer would often be invalidated as the source code changed. 
# A consequence of this is that we have to run go mod tidy outside of 
# the docker compose file to ensure that any added dependencies are 
# present in the .mod and .sum files
RUN go mod download
# copy in the source code
COPY . .
# build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/main ./cmd/server/main.go 

FROM alpine/curl:latest AS runner
WORKDIR /app
# copy the binary into the runner
COPY --from=builder /app/main .
RUN adduser -D appuser
USER appuser
# start the binary
CMD ["./main"]
FROM golang:1.24 AS builder
# copy in the dependencies list and download the dependencies
WORKDIR /app
COPY go.mod go.sum ./
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
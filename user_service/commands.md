## Generating Server Stubs
```bash
cd .
export PATH="$PATH:$(go env GOPATH)/bin"
protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative api/user.proto
```

## Brief note on testing philosophy:
- for the purpose of this project a unit test has mocked dependencies and an integration test has concrete dependencies
- integration tests make heavy use of testcontainers

## Run all Tests (unit and integration)
```bash
go test ./...
```

## Run just unit test
```bash
go test ./.. -v -run '^Test.*Unit$'
```

## Run just integration tests
```bash
go test ./... -v -run '^Test.*Integration$'
```
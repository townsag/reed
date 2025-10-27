## Run the unit and integration tests with coverage profiling and view coverage by file
- running these commands from the root directory of this project (the same place that this file is) ensures that all the necessary packages are tested and any new packages added in the future are tested
```bash
cd .
go test ./... -coverpkg=./... -coverprofile=coverage.out
go tool cover -html=coverage.out
```

## Run all tests
```bash
cd .
go test ./...
```

## Run just the unit tests
```bash
cd .
go test ./... -v -run 'Test.*Integration$'
```

## Run just the integration tests
```bash
cd .
go test ./... -v -run 'Test.*Unit$'
```

## Generating SQLC code
```bash
cd ./internal/repository/sqlc
sqlc generate
```
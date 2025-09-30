## Resources:
- gRPC basics:
    - https://grpc.io/docs/languages/go/quickstart/
    - https://grpc.io/docs/what-is-grpc/core-concepts/
    - https://grpc.io/docs/languages/go/basics/

## Conceptual:
- grpc abstracts away the process of defining handlers and validation the input to routes
    - handler code is generated automatically
    - validation is done automatically
- gRPC supports four message paradigms:
    - request response
        - called unary rpc
    - request -> stream response
        - called server streaming rpc
    - stream request -> response
        - called client streaming rpc
    - bidirectional streams
        - called bidirectional streaming rpc
- grpc error handling:
    - https://grpc.io/docs/guides/error/
- protobuf:
    - protocol buffers can be used to store data either on disk or when sent over the network
    - protocol buffers allow for changes in the message schema, both backwards and forwards compatible
    - language agnostic
    - limited in size to around 1mb
    - messages are not comparable when serialized
    - message fields can be either optional or implicit:
        - optional:
            - can check if the field has been set or left blank
        - implicit:
            - cannot tell if the field was set or left with a zero value
    - binary wire unsafe changes:
        - changing field numbers
        - moving fields into an existing oneOf
    - binary wire safe changes:
        - adding fields
        - removing fields
        - adding values to an enum
        - adding an existing field to a new oneOf
        - changing a oneOf with only one field into an explicit presence field (optional)

## Generating Server Stubs
```bash
cd .
export PATH="$PATH:$(go env GOPATH)/bin"
protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative api/user.proto
```

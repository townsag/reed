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



Consider this directory structure:
myproject/
├── cmd/
│   ├── server/
│   │   └── main.go          # Server entry point
│   └── client/
│       └── main.go          # Client entry point (if needed)
├── api/
│   └── proto/
│       └── v1/
│           └── service.proto # Proto definitions
├── pkg/
│   └── api/
│       └── v1/
│           ├── service.pb.go       # Generated protobuf code
│           └── service_grpc.pb.go  # Generated gRPC code
├── internal/
│   ├── server/
│   │   └── server.go        # gRPC server implementation
│   ├── service/
│   │   └── service.go       # Business logic
│   ├── repository/
│   │   └── repository.go    # Data access layer
│   └── config/
│       └── config.go        # Configuration
├── Makefile                  # Build and proto generation commands
├── go.mod
└── go.sum


## Go Best Practices to consider:
- accept interfaces and return structs:
    - https://bryanftan.medium.com/accept-interfaces-return-structs-in-go-d4cab29a301b
    - the consuming package should receive its dependencies as an interface
        - this allows the consumer to define the interface that it receives
    - the producing package should return a concrete type so that the consumer 
      can more accurately reason about the returned value
- export the field of a struct and the new method to create a struct but don't
  export the struct itself so that any calling code that wants to use an instance
  of that struct has to use the new method to create an instance of the struct
- the internal package is enforced by the golang toolchain as a place for things 
  that are not publicly available
- the pkg package is a convention for things that are part of the public api of your
  service and are meant to be used by other servers
    - examples:
        - convenient warpers for generated grpc client
        - domain models
        - domain errors
        - validation logic
        - event definitions

## TODO:
- [ ] client:
    - client is generated with grpc? Do I just have to call generated stubs
- [ ] middleware
    - example:
        - https://github.com/grpc/grpc-go/tree/master/examples/features/interceptor
    - common middleware:
        - https://github.com/grpc-ecosystem/go-grpc-middleware
            - look here for both tracing and metrics interceptors
    - [ ] request id
        - return the request id in either the response or in the error status details
        - alternatively return the request id in the response headers
        - look for request id in google standard protobuf
    - logging
- [ ] observability tools
    - [ ] tracing
    - [ ] metrics
    - [ ] logging aggregation
- [ ] integration testing at the server level
    - use test containers to create an instance of the user service and run calls against it
    - documentation for creating custom images for test containers
        - https://golang.testcontainers.org/features/creating_container/
- [ ] unit testing at the service level:
    - [ ] create a mock repository and run tests against the service which uses the mock repository
    - [ ] create a mock stream and run tests against the service verifying that it publishes messages to the mock stream
- [ ] write user updates to a stream:
    - integrate NATs
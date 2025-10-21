## TODO:
- create a gRPC server which serves state about documents
- decide on a domain model for what information is stored by the document service:
    - Decided:
        - document metadata like
    - Questions
- understand the programming model for having a service that both reads from a stream and serves gRPC request
    - two separate binaries that either serve gRPC requests or apply events to a database
        - components of the service can be scaled independently
        - more operational complexity
        - two binaries that are using the same database, may require simultaneous deployment of the two binaries
        - serving requests may be cpu bound and processing events may be memory bound, we can scale the two independently with different requirements
        - add local cache for data locality
    - one binary that both reads from event stream and serve gRPC requests
        - partitioning of the stream might be tightly coupled to the request load and vice versa
        - might have to hash requests by document id to match the sharding of the document event workers
        - high data locality if we cache documents on the server instance that both consumes events and serves gRPC requests
        - requires partition aware routing
        - can only scale to as many instances as we have partitions 
- update the uuid generation code to handle errors when generating uuids instead of panicking
- update the document repository interface to return both Permission and PermissionLevel types when calling list permissions on document:
  - Permission is a struct with metadata about the permission including the recipient type and the permission level
  - PermissionLevel is just the permission level itself
- update the repo creation process so that custom types are registered with the pgx postgres client library at repo creation time instead of at postgres connection creation time

## Potential Directory Structure:
/document_service
  /cmd
    /api-server           # Entrypoint for API process
      main.go
    /worker              # Entrypoint for worker process
      main.go
  /internal
    /domain              # Shared business logic
      document.go
      repository.go
    /storage             # Shared data access
      postgres.go
      migrations/
    /grpc                # API-specific code
      handler.go
    /stream              # Worker-specific code
      consumer.go
  /migrations            # Single source of schema truth
    001_initial.sql
    002_add_metadata.sql

## Data storage Postgres
- needs:
  - documents
    - metadata
      - created by
      - created at
      - updated by
      - updated at
    - permissions
      - this is a zero or many to one or many relationship because:
        - a user can be associated with zero or many documents
        - a document can be associated with one or many users
    - content
  - users?
    - is this what they call event sourcing?
    - do I need to keep track of which users are available in the documents database so that I can ensure that documents are always associated with at least one user
    - should a document be deleted if its owner is deleted? probably not
    - should I be able to make multiple users owners of a document? probably
    - should I check that a user is not deleted before updating its permissions status

- how do I make the application of events idempotent?
- what does it mean for someone to be an owner of a document?
- anonymous access: 
  - option 1: create one anonymous user and assign permissions to that user
    - pros:
      - simple
    - cons:
      - updates to a user in the future cant be partitioned on user id because all anonymous users will have the same user id
      - does not allow for fine grained access
  - option 2: specify anonymous permissions on the document itself
    - pros:
      - simple
      - no coupling with the users service
    - cons:
      - have to query two tables to see who has access to a document
      - does not allow for fine grained access
      - nowhere to store metadata about the anonymous user
  - option 3: create a new anonymous user for each shared link
    - pros:
      - allows for multiple types of access by anonymous users
      - fine grain track of which links have been issued and the potential to provide fine grained control over the permissions of links
    - cons:
      - users table is polluted by extra anonymous users
        - maybe the principals table is stored in the document service and the users table is stored in the users service? 
      - coupling with the users service for anonymous access
      - coupling with the users service for generating ids
        - either I have to generate uuids for the anonymous user or I have to get an id from the users service
      - how do we promote from an anonymous user to an authenticated user
  - is it worth it to split the users table into users and principals with all users being principals but not all principals being users
  - choice:
    - create a principals table that is owned by the documents service
    - 

CREATE TABLE documents (
  id UUID PRIMARY KEY,
  created_at TIMESTAMP NOT NULL,
  created_by INT32 NOT NULL,
  last_modified TIMESTAMP
);

CREATE TABLE principals (
  id UUID PRIMARY KEY,
  name TEXT,
  description TEXT
);

CREATE TYPE permission_recipient AS ENUM ('user', 'anonymous');
CREATE TYPE permission_level AS ENUM ('viewer', 'editor', 'owner');

CREATE TABLE permissions {
  document_id UUID NOT NULL,
  recipient_id UUID NOT NULL,
  type permission_recipient,
  PRIMARY KEY (document_id)
};

- consider adding support for security groups

## Thoughts on pagination and api design:
- pagination is necessary when retrieving more than a trivial number of items
- offset based pagination is not suitable whenever the set of things that is being paginated over is changing
  - this is because updates can result in the location of elements in a page to shift, or which page an element can end up in
  - this can cause calling client code to never see old items that have always been there
- the latency associated with offset based pagination grows linearly with the size of the offset
- this is one of the reasons that cursor based pagination is useful, it prevents the possibility of "lost writes" from the perspective of the client
  - using cursor based pagination to scan an ordered index on a table forces us to start from the spot that we left off at next time we return a value, even if the number of elements in the table on either side of the last visited element has changed

## Events to send:
- document is deleted
  - still need to figure out what it means for a document to be deleted, like do these things get permanently deleted? Can a user still be the owner of a document if that document is marked for deletion
- permission is updated 
  - the case where a user is already actively interacting with the message proxy service via a websocket connection and we need to either increase their permissions or close the connection
  - should start warming up the cache for a document because it is likely that we will have a new connection for that document
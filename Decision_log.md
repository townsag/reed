- Access patterns for lists of documents:
    - use cursor based pagination instead of using offset based pagination
    - the set of documents shared with a user could change quickly, if we use offset based pagination and the set of documents shared with a user changes then the user might not see some documents at all or might see some documents multiple times when linearly traversing through the documents
        - traverse the index of a document on some indexed value like created at, use the document id as the tiebreaker for documents created at the same time
    - provide a useful was to search documents:
        - sorting:
            - support few different types of sorting because each field that we sort on has to als be a field that we index on. This is an implementation detail of cursor based pagination.
        - for documents that a user owns:
            - filters:
                - shared with a destination user by the current user
        - for documents that are shared with a user:
            - filters:
                - shared with the current user by a source user
        - regardless of ownership:
            - filters
                - last modified at
                - created at
            - queries:
                - text search
- Logging:
    - the slog logger.WithAttributes pattern can be used to bind key value pairs to a logger so that all subsequent calls to that logger have those key value pairs
        - this is very useful to manually bind the request id to a logger
    - this pattern is not necessary when using otelslog because otelslog will automatically extract the spanId and traceId from the context when logging
        - this way all the logs for a trace are bound to a trace id
        - this only works when we use the logger.InfoContext(ctx...) functions
    - this way we can use the trace viewer to find all the traces with a userId attribute and then look at logs with those trace ids instead of looking for all the logs with a user id
    - fundamentally: Tempo is the entrypoint for observability, see logging as a secondary source of observability data
        - it is not important that logs are easily queryable if Tempo is easily queryable and the logs can easily be reached from tempo

- fine grain permissions:
    - sets of resources have authorization rules, like someone with an anonymous token cannot 
      create any documents, this is a course grain authorization rule
    - specific resources have permissions, like someone with an anonymous token might have editor
      privileges on some resources but not on other resources
    - the api gateway layer should be in charge of coarse grain authorization like which types
      of routes a token is able to call
    - the service / backend layer behind the gateway should be in charge of fine grain permissions
      like which tokens can access which specific resources
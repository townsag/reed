## Functional Spec:
- when looking at a request, I should be able to trace the path of that request through multiple services without having to open many different log pages and without having to know what log pages to open
- this requires instrumenting all of the services that would participate in responding to a request with opentelemetry tracing

## Technical Spec:
- instrument the document service with opentelemetry tracing:
    - instrument the grpc library so that we can measure request response latencies
    - instrument the postgres client library so that we can see which queries are made and how long those queries take
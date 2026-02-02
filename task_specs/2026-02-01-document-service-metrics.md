## Description:
- add metrics to the document service
- I should be able to jump from the metrics for a service to a corresponding trace associated with that value of that metric

## Technical requirements:
- this is the flow that metrics will take through the infra:
    - application -> otel collector -> prometheus -> grafana ui
- [ ] bootstrap the metrics provider for the otel sdk
- [ ] add instrumentation libraries for various clients
    - [ ] grpc service otel metrics instrumentation
    - [ ] postgres client / connection pool otel metrics instrumentation

## Resources:
- for linking metrics to traces:
    - introduction to exemplars:
        - https://grafana.com/docs/grafana/latest/fundamentals/exemplars/
        - not very useful
    - configuring prometheus to save exemplars
        - prometheus configuration documentation:
            - https://prometheus.io/docs/prometheus/latest/configuration/configuration/#exemplars
    - configuring grafana datasource to show exemplars
        - https://grafana.com/docs/grafana/latest/datasources/prometheus/configure/#provision-the-prometheus-data-source
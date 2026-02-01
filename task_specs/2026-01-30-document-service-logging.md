## Functional Description:
- when debugging, I should be able to see all the necessary logs in grafana
- these logs should be associated with traces
- these logs should be instance scoped but I should also be able to see all the logs for a service / deployment
- these logs should be viewable in stdout of the running instance as well 

## Technical tasks:
- instrument the document service with otel logging 
    - add the log exported to the otel bootstrap process 
- create a globally available logger
    - modify the globally available logger such that it is backed by the otel logger provider
    - configure the otel logger provider such that it prints to both std out and to grafana
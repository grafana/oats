# Test suite for the Java OpenTelemetry distribution
  
The java tests declare the expected outcome declarative in a `oats.yaml` files.

This is an example:

```yaml
expected:
  metrics:
    - promql: 'db_client_connections_max{pool_name="HikariPool-1"}'
      value: "== 10"
  dashboards:
    - path: ../jdbc-dashboard.json
      panels:
        - title: Connection pool waiting requests
          value: "== 0"
        - title: Connection pool utilization
          value: "> 0"
```
                                                              
You have to provide the root path of the java distributions example directory to ginkgo 
via the environment variable `JAVA_TESTCASE_BASE_PATH`.

## Starting the Java Suite

```bash
JAVA_TESTCASE_BASE_PATH=/path/to/grafana-opentelemetry-java/examples ginkgo -v -r
```
                           

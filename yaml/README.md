# Declarative Yaml tests

You can use declarative yaml tests in `oats.yaml` files:

This is an example:

```yaml
docker-compose:
  generator: java
  file: ../docker-compose.yaml
  resources:
    - kafka
input:
  - url: http://localhost:8080/stock
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
via the environment variable `TESTCASE_BASE_PATH`.

## Docker Compose

Describes the docker-compose file to use for the test.
The files typically defines the instrumented application you want to test and optionally some dependencies,
e.g. a database server to send requests to.
You don't need (and should have) to define the observability stack (e.g. prometheus, grafana, etc.),
because this is provided by the test framework (and may test different versions of the observability stack,
e.g. otel collector and grafana agent).

This docker-compose file is relative to the `oats.yaml` file.
If you're referencing other configuration files, you can use the `resources` field to specify them.

### Generators

Generators can be used to generate a docker-compose file from a template as a way to avoid repetition.
Currently, the only generator available is `java` which generates a docker-compose file for the java distribution
examples.

## Starting the Java Suite

```bash
TESTCASE_BASE_PATH=/path/to/grafana-opentelemetry-java/examples ginkgo -v -r
```
                           
You can increase the timeout, which is useful if you want to inspect the telemetry data manually
in grafana at http://localhost:3000

```
TESTCASE_TIMEOUT=1h TESTCASE_BASE_PATH=/path/to/grafana-opentelemetry-java/examples ginkgo -v -r
```

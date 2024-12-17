# OpenTelemetry Acceptance Tests (OATs)

OpenTelemetry Acceptance Tests (OATs), or OATs for short, is a test framework for OpenTelemetry.

- Declarative tests written in YAML
- Supported signals: traces, logs, metrics
- Full round-trip testing: from the application to the observability stack
  - Data is stored in the LGTM stack ([Loki], [Grafana], [Tempo], [Prometheus], [OpenTelemetry Collector])
  - Data is queried using LogQL, PromQL, and TraceQL
  - All data is sent to the observability stack via OTLP - so OATS can also be used with other observability stacks
- End-to-end testing
  - Docker Compose with the [docker-otel-lgtm] image
  - Kubernetes with the [docker-otel-lgtm] and [k3d]

Under the hood, OATs uses [Ginkgo] and [Gomega] to run the tests.

## Getting Started

> You can use the test cases in [prom_client_java](https://github.com/prometheus/client_java/tree/main/examples/example-exporter-opentelemetry/oats-tests) as a reference.
> The [GitHub action](https://github.com/prometheus/client_java/blob/main/.github/workflows/acceptance-tests.yml)
> uses a [script](https://github.com/prometheus/client_java/blob/main/scripts/run-acceptance-tests.sh) to run the tests.

1. Create a folder `oats-tests` for the following files
2. Create `Dockerfile` to build the application you want to test
    ```Dockerfile         
    FROM eclipse-temurin:21-jre
    COPY target/example-exporter-opentelemetry.jar ./app.jar
    ENTRYPOINT [ "java", "-jar", "./app.jar" ]
    ```
3. Create `docker-compose.yaml` to start the application and any dependencies
    ```yaml         
    version: '3.4'
    
    services:
      java:
        build:
          dockerfile: Dockerfile
        environment:
          OTEL_SERVICE_NAME: "rolldice"
          OTEL_EXPORTER_OTLP_ENDPOINT: http://lgtm:4318
          OTEL_EXPORTER_OTLP_PROTOCOL: http/protobuf
          OTEL_METRIC_EXPORT_INTERVAL: "5000"  # so we don't have to wait 60s for metrics
    ```
4. Create `oats.yaml` with the test cases
    ```yaml         
    # OATS is an acceptance testing framework for OpenTelemetry - https://github.com/grafana/oats
    docker-compose:
      files:
        - ./docker-compose.yaml
    expected:
      metrics:
        - promql: 'uptime_seconds_total{}'
          value: '>= 0'
    ```
5. `cd /path/to/oats/yaml` 
6. `go install github.com/onsi/ginkgo/v2/ginkgo`
7. `TESTCASE_BASE_PATH=/path/to/oats-tests ginkgo -v`

## Test Case Syntax

> You can use any file name that matches `oats*.yaml` (e.g. `oats-test.yaml`), that doesn't end in `-template.yaml`.
> `oats-template.yaml` is reserved for template files, which are used in the "include" section.

The syntax is a bit similar to https://github.com/kubeshop/tracetest

This is an example:

```yaml
include:
  - ../oats-template.yaml
docker-compose:
  file: ../docker-compose.yaml
input:
  - url: http://localhost:8080/stock
interval: 500ms
expected:
  traces:
    - traceql: '{ name =~ "SELECT .*product"}'
      spans:
        - name: 'regex:SELECT .*'
          attributes:
            db.system: h2
  logs:
    - logql: '{exporter = "OTLP"}'
      contains: 
        - 'hello LGTM'
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

You have to provide the root path of the directory where your test cases are located to ginkgo
via the environment variable `TESTCASE_BASE_PATH`.

## Docker Compose

Describes the docker-compose file(s) to use for the test.
The files typically defines the instrumented application you want to test and optionally some dependencies,
e.g. a database server to send requests to.
You don't need (and should have) to define the observability stack (e.g. prometheus, grafana, etc.),
because this is provided by the test framework (and may test different versions of the observability stack,
e.g. otel collector and grafana agent).

This docker-compose file is relative to the `oats.yaml` file.

## Kubernetes

A local kubernetes cluster can be used to test the application in a kubernetes environment rather than in docker-compose.
This is useful to test the application in a more realistic environment - and when you want to test Kubernetes specific features.

Describes the kubernetes manifest(s) to use for the test.

```yaml
kubernetes:
  dir: k8s
  app-service: dice
  app-docker-file: Dockerfile
  app-docker-context: ..
  app-docker-tag: dice:1.1-SNAPSHOT
  app-docker-port: 8080
```

## Matrix of test cases

Matrix tests are useful to test different configurations of the same application, 
e.g. with different settings of the otel collector or different flags in the application.

```yaml
matrix:
  - name: new
    docker-compose:
  - name: old-jvm-metrics
    docker-compose:
input:
  - path: /stock
```

## Debugging

If you want to run a single test case, you can use the `--focus` option:

```sh
TESTCASE_BASE_PATH=/path/to/project ginkgo -v --focus="jdbc"
```

You can increase the timeout, which is useful if you want to inspect the telemetry data manually
in grafana at http://localhost:3000

```sh
TESTCASE_TIMEOUT=1h TESTCASE_BASE_PATH=/path/to/project ginkgo -v
```

You can keep the container running without executing the tests - which is useful to debug in grafana manually:

```sh
TESTCASE_MANUAL_DEBUG=true TESTCASE_BASE_PATH=/path/to/project ginkgo -v
```

[Ginkgo]: https://onsi.github.io/ginkgo/
[Gomega]: https://onsi.github.io/gomega/
[Tempo]: https://github.com/grafana/tempo
[OpenTelemetry Collector]: https://opentelemetry.io/docs/collector/ 
[Prometheus]: https://prometheus.io/
[Grafana]: https://grafana.com/
[Loki]: https://github.com/grafana/loki
[docker-otel-lgtm]: https://github.com/grafana/docker-otel-lgtm/
[k3d]: https://k3d.io/


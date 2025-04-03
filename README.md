# OpenTelemetry Acceptance Tests (OATs)

OpenTelemetry Acceptance Tests (OATs), or OATs for short, is a test framework for OpenTelemetry.

- Declarative tests written in YAML
- Supported signals: traces, logs, metrics
- Full round-trip testing: from the application to the observability stack
  - Data is stored in the LGTM stack ([Loki], [Grafana], [Tempo], [Prometheus], [OpenTelemetry Collector])
  - Data is queried using LogQL, PromQL, and TraceQL
  - All data is sent to the observability stack via OTLP - so OATs can also be used with other observability stacks
- End-to-end testing
  - Docker Compose with the [docker-otel-lgtm] image
  - Kubernetes with the [docker-otel-lgtm] and [k3d]

## Installation

1. Install the `oats` binary:

```sh
go install github.com/grafana/oats@latest
```

2. You can confirm it was installed with:

```sh
❯ ls $GOPATH/bin
oats
```

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
    # OATs is an acceptance testing framework for OpenTelemetry - https://github.com/grafana/oats
    docker-compose:
      files:
        - ./docker-compose.yaml
    expected:
      metrics:
        - promql: 'uptime_seconds_total{}'
          value: '>= 0'
    ```
5. Run the tests:
```sh
oats /path/to/oats-tests/oats.yaml
```

## Running OATs Directly

OATs can be run directly using the command-line interface:

```sh
# Basic usage
go run main.go /path/to/oats-tests/oats.yaml

# With flags
go run main.go --timeout=1m --lgtm-version=latest --manual-debug=false /path/to/oats-tests/oats.yaml
```

The following flags are available:

- `-timeout`: Set the timeout for test cases (default: 30s)
- `-lgtm-version`: Specify the version of [docker-otel-lgtm] to use (default: "latest")
- `-manual-debug`: Enable debug mode to keep containers running (default: false)

## Test Case Syntax

> You can use any file name that matches `oats*.yaml` (e.g. `oats-test.yaml`), that doesn't end in `-template.yaml`.
> `oats-template.yaml` is reserved for template files, which are used in the `include` section.

The syntax is a bit similar to https://github.com/kubeshop/tracetest

This is an example:

```yaml
include:
  - ../oats-template.yaml
docker-compose:
  file: ../docker-compose.yaml
input:
  - url: http://localhost:8080/stock
interval: 500ms # interval between requests to the input URL
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
  dashboards: # Grafana dashboards
    - path: ../jdbc-dashboard.json
      panels:
        - title: Connection pool waiting requests
          value: "== 0"
        - title: Connection pool utilization
          value: "> 0"
```

### Query traces

Each entry in the `traces` array is a test case for traces.

```yaml
expected:
  traces:
    - traceql: '{ name =~ "SELECT .*product"}'
      spans:
        - name: 'regex:SELECT .*' # regex match
          attributes:
            db.system: h2
          allow-duplicates: true # allow multiple spans with the same attributes
```

### Query logs

Each entry in the `logs` array is a test case for logs.

```yaml
expected:
  logs:
    - logql: '{service_name="rolldice"} |~ `Anonymous player is rolling the dice.*`'
      equals: 'Anonymous player is rolling the dice'
      attributes:
        service_name: rolldice
      attribute-regexp:  
        container_id: ".*"
      no-extra-attributes: true # fail if there are extra attributes
    - logql: '{service_name="rolldice"} |~ `Anonymous player is rolling the dice.*`'
      regexp: 'Anonymous player is .*'
```

### Query metrics

```yaml
expected:
  metrics:
    - promql: 'db_client_connections_max{pool_name="HikariPool-1"}'
      value: "== 10"
  dashboards: # Useful if you populate Grafana dashboards from JSON
    - path: ../jdbc-dashboard.json 
      panels:
        - title: Connection pool waiting requests
          value: "== 0"
        - title: Connection pool utilization
          value: "> 0"
```

## Docker Compose

Describes the docker-compose file(s) to use for the test.
The files typically define the instrumented application you want to test and optionally some dependencies,
e.g. a database server to send requests to.
You don't need (and shouldn't have) to define the observability stack (e.g. Prometheus, Grafana, etc.),
because this is provided by the test framework (and may test different versions of the observability stack,
e.g. OTel Collector and Grafana Alloy).

This docker-compose file is relative to the `oats.yaml` file.

## Kubernetes

A local Kubernetes cluster can be used to test the application in a Kubernetes environment rather than in docker-compose.
This is useful to test the application in a more realistic environment - and when you want to test Kubernetes specific features.

Describes the Kubernetes manifest(s) to use for the test.

```yaml
kubernetes:
  dir: k8s
  app-service: dice
  app-docker-file: Dockerfile
  app-docker-context: ..
  app-docker-tag: dice:1.1-SNAPSHOT
  app-docker-port: 8080
```


[Tempo]: https://github.com/grafana/tempo
[OpenTelemetry Collector]: https://opentelemetry.io/docs/collector/ 
[Prometheus]: https://prometheus.io/
[Grafana]: https://grafana.com/
[Loki]: https://github.com/grafana/loki
[docker-otel-lgtm]: https://github.com/grafana/docker-otel-lgtm/
[k3d]: https://k3d.io/


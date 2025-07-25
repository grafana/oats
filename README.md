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

> [!TIP]
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

## Running multiple tests

It can run multiple tests:

```sh
oats /path/to/repo
```

This will search all subdirectories for test files. The tests are defined in `oats*.yaml` files.

## Flags 

The following flags are available:

- `-timeout`: Set the timeout for test cases (default: 30s)
- `-lgtm-version`: Specify the version of [docker-otel-lgtm] to use (default: `"latest"`)
- `-manual-debug`: Enable debug mode to keep containers running (default: `false`)
- `-lgtm-log-all`: Enable logging for all containers (default: `false`)
- `-lgtm-log-grafana`: Enable logging for Grafana (default: `false`)
- `-lgtm-log-loki`: Enable logging for Loki (default: `false`)
- `-lgtm-log-tempo`: Enable logging for Tempo (default: `false`)
- `-lgtm-log-prometheus`: Enable logging for Prometheus (default: `false`)
- `-lgtm-log-pyroscope`: Enable logging for Pyroscope (default: `false`)
- `-lgtm-log-otel-collector`: Enable logging for OpenTelemetry Collector (default:`false`)
- `-host`: Override the host used to issue requests to applications and LGTM (default:`localhost`)

## Run OATs in GitHub Actions

Here's a [script](https://github.com/grafana/docker-otel-lgtm/blob/main/scripts/run-acceptance-tests.sh) that is used
from GitHub Actions. It uses [mise](https://mise.jdx.dev/) to install OATs, but you also [install OATs directly](#installation).

## Test Case Syntax

> [!TIP]
> You can use any file name that matches `oats*.yaml` (e.g. `oats-test.yaml`), that doesn't end in `-template.yaml`.
> `oats-template.yaml` is reserved for template files, which are used in the `include` section.

The syntax is a bit similar to [Tracetest](https://github.com/kubeshop/tracetest).

Here is an example:

```yaml
include:
  - ../oats-template.yaml
docker-compose:
  file: ../docker-compose.yaml
input:
  - path: /stock
    status: 200 # expected status code, 200 is the default
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
```

Here is another example with a more specific input:

```yaml
include:
  - ../oats-template.yaml
docker-compose:
  file: ../docker-compose.yaml
input:
  - path: /users
    method: POST
    scheme: https
    host: 127.0.0.1
    status: 201
    headers:
      Authorization: Bearer my-access-token
      Content-Type: application/json
    body: |-
      {
        "name": "Grot"
      }
interval: 500ms
expected:
  traces:
    - traceql: '{ name =~ "SELECT .*product"}'
      spans:
        - name: 'regex:SELECT .*'
          attributes:
            db.system: h2
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
```

### Matrix of test cases

Matrix tests are useful to test different configurations of the same application,
e.g. with different settings of the otel collector or different flags in the application.

```yaml
matrix:
  - name: default
    docker-compose:
      files:
        - ./docker-compose.oats.yml
  - name: self-contained
    docker-compose:
      files:
        - ./docker-compose.self-contained.oats.yml
  - name: net8
    docker-compose:
      files:
        - ./docker-compose.net8.oats.yml
```

You can then make test cases depend on the matrix name:

```yaml
expected:
  metrics:
    - promql: 'db_client_connections_max{pool_name="HikariPool-1"}'
      value: "== 10"
      matrix-condition: default
```

`matrix-condition` is a regex that is applied to the matrix name.

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

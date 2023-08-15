## dockertest

Examples utilizing [dockertest][] to provision self contained telemetry pipelines for use in integration testing.

[dockertest]: https://github.com/ory/dockertest

## Goals

1. Self contained, programmatic provisioning of [Tempo][], the [OpenTelemetry Collector][], and [Prometheus][] with [dockertest][].
1. Straightforward support for externally provisioned observability endpoints.
1. Opportunity to learn how to stand up the services the Application Observability product suite depends on.

[Tempo]: https://github.com/grafana/tempo
[OpenTelemetry Collector]: https://github.com/open-telemetry/opentelemetry-collector
[Prometheus]: https://github.com/prometheus/prometheus

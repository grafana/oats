## OpenTelemetry Acceptance Tests (OATs)

### Goals

1. Flexibility to support qualification of changes to the [OpenTelemetry Collector][], and [Tempo][]
1. Ability to support OpenTelemetry SDK functionality such as [sampling][]
1. Self contained, programmatic provisioning of [Tempo][], the [OpenTelemetry Collecor][], and [Mimir][] with [dockertest][]
1. Straightforward support for externally provisioned 
1. Highlight the use of [Ginkgo][], and [Gomega][]
1. Have a cute name

[Tempo]: https://github.com/grafana/tempo
[OpenTelemetry Collector]: https://github.com/open-telemetry/opentelemetry-collector
[Mimir]: https://github.com/grafana/mimir
[dockertest]: https://github.com/ory/dockertest
[sampling]: https://opentelemetry.io/docs/instrumentation/go/sampling/
[Ginkgo]: https://onsi.github.io/ginkgo/
[Gomega]: https://onsi.github.io/gomega/

### Getting Started

1. Install [Go][]
1. Install [Docker][] ([Podman][] also works provided it is listening on the expected Docker Unix socket)
1. Clone the repository
1. Ensure that `${GOBIN}` is on your `${PATH}`
1. From within the repository directory, install [Ginkgo][]

```
go install github.com/onsi/ginkgo/v2/ginkgo
```

1. Run the specs

```
ginkgo -r (or ginkgo ./...)
```

1. Browse the [examples][]

[Go]: https://go.dev/
[Docker]: https://www.docker.com/
[Podman]: https://podman.io/
[examples]: examples/README.md

### Writing Specs

1. Decide whether to use the `testhelpers/observability` package, individual packages such as
   `testhelpers/tempo`, or only support externally provisioned endpoints
1. Write the specs using [Ginkgo][], and [Gomega][]
1. Profit

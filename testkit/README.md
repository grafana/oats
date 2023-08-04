# TestKit

TestKit is a collection of test suites and utilities intended to provide verification for the App O11y data ingestion pipeline.
The purpose of this module is to create unit-style test suites that can run without external dependencies. Unit testing specific
components directly allows debugging of components under test with ease.

## Data Model

See the following resources for more information.

- [Design Doc: Service Graph and RED Metrics data model / Ingestion pipeline data model]
- [Data Model Doc: RED and Service Graph Metrics]

## Running Tests

The test suites are built using the [Ginkgo Testing Framework]. Install the ginkgo executable before running

```sh
go install github.com/onsi/ginkgo/v2/ginkgo
```

To execute all test suites in the repository, run

```sh
make run
```

To run an individual suite, use `ginkgo` directly

```sh
ginkgo run ./servicegraph/
```

[design doc: service graph and red metrics data model / ingestion pipeline data model]: https://docs.google.com/document/d/1xsl1-xue5LaFYb6SlzGvcSr_CG97_ohe-y84cql1FEM/edit
[data model doc: red and service graph metrics]: https://docs.google.com/document/d/1LtFziczOwD_9TyoEkq5S_g8t51VCsX_P2sjkYrKA3Kk/edit
[ginkgo testing framework]: https://onsi.github.io/ginkgo/

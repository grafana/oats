# TestKit

TestKit is a collection of test suites and utilities intended to provide verification for the App O11y data ingestion pipeline.
The purpose of this module is to create unit-style test suites that can run without external dependencies. Unit testing specific
components directly allows debugging of components under test with ease.

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

[ginkgo testing framework]: https://onsi.github.io/ginkgo/

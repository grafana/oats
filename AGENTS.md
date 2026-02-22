# AGENTS.md

This file provides guidance to AI coding agents when working with code in this repository.

## Project Overview

OATs (OpenTelemetry Acceptance Tests) is a declarative test framework for
OpenTelemetry written in Go. It enables full round-trip testing from
instrumented applications to the observability stack (LGTM: Loki, Grafana,
Tempo, Prometheus, Mimir, OpenTelemetry Collector).

Test cases are defined in YAML files with `oats-schema-version: 2` and can
validate traces (TraceQL), logs (LogQL), metrics (PromQL), and profiles
(Pyroscope queries) using Docker Compose or Kubernetes backends.

## Build Commands

```bash
# Build
mise run build

# Run unit tests
mise run test

# Run integration tests (requires Docker)
mise run integration-test

# Run end-to-end tests
mise run e2e-test

# Run all checks (lint + test + format check)
mise run check
```

## Linting

```bash
# Auto-fix and verify (recommended dev workflow)
mise run fix

# Verify only (same command used in CI)
mise run lint

# Go linting only
mise run lint:go

# Format Go code
mise run fmt
```

Lint tasks are sourced from [grafana/flint](https://github.com/grafana/flint).

## Architecture

### Package Organization

- **`main.go`** — CLI entry point. Parses flags, discovers YAML test files, runs them sequentially
- **`model/`** — Core data models (`TestCaseDefinition`, expected signals)
- **`yaml/`** — Test case parsing, execution, signal-specific assertions (`runner.go`, `traces.go`, `metrics.go`, `logs.go`, `profiles.go`)
- **`testhelpers/`** — Docker Compose management, Kubernetes (k3d), HTTP request helpers, response parsing
- **`observability/`** — Observability endpoint interface
- **`tests/`** — Integration and e2e test fixtures

### Test Case Schema

Required fields:
- `oats-schema-version: 2` (must be present in all test files)
- `oats-template: true` (for template files used in `include`)

Core sections: `include`, `docker-compose`, `kubernetes`, `matrix`, `input`, `interval`, `expected`

File discovery scans for `.yaml`/`.yml` files containing `oats-schema-version`. Files with `oats-template: true` are skipped as entry points. `.oatsignore` causes a directory to be ignored.

## CLI Usage

```bash
# Run specific test file
oats /path/to/test.yaml

# Scan directory for all tests
oats /path/to/tests/

# With flags
oats -timeout 1m -lgtm-version latest /path/to/test.yaml
```

Key flags: `-timeout` (default 30s), `-lgtm-version` (default "latest"), `-manual-debug` (keep containers running)

## Code Conventions

- Go 1.24+
- Assertions: gomega
- YAML parsing: `go.yaml.in/yaml/v3` with strict field validation
- Logging: `log/slog`
- Docker: `docker compose` (not legacy `docker-compose`)

## Testing

- Unit tests: `mise run test`
- Integration tests require `INTEGRATION_TESTS=true` env var
- Uses gomega for assertions, stretchr/testify for test utilities

## CI

- Build + lint + test on PRs (`mise run check`)
- Integration tests and e2e tests in separate workflows
- Linting via flint (super-linter, lychee, golangci-lint)

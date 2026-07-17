# AGENTS.md

This file provides guidance to AI coding agents when working with code in this repository.

For a broader contributor orientation, see [docs/repository-guide.md](docs/repository-guide.md).

## Project Overview

OATs (OpenTelemetry Acceptance Tests) is a declarative test framework for
OpenTelemetry written in Go. It enables full round-trip testing from
instrumented applications to the observability stack (LGTM: Loki, Grafana,
Tempo, Prometheus, Mimir, OpenTelemetry Collector).

Test cases are defined in YAML files, discovered through an `oats-config.yaml`
whose `meta.version: 3` is the single schema version (cases carry no version
field of their own). They can validate traces (TraceQL), logs (LogQL), metrics
(PromQL), and profiles (Pyroscope queries) using Docker Compose or Kubernetes
backends.

## Build Commands

```bash
# Build
mise run build

# Run non-e2e tests
mise run test

# Run e2e tests
mise run e2e-test
```

## Linting

```bash
# Auto-fix lint issues (recommended dev workflow)
mise run lint:fix

# Verify only (same command used in CI)
mise run lint

# Run all checks
mise run check
```

Linting is handled by [flint](https://github.com/grafana/flint).
Flint runs shellcheck, shfmt, actionlint, hadolint, rumdl, taplo, ryl,
biome, typos, editorconfig, lychee, renovate-deps, and gofmt.
EditorConfig rules live in `.editorconfig`.

## Architecture

### Package Organization

- **`main.go`** — Root `oats` CLI entry point
- **`internal/cli/`** — The gcx-driven CLI implementation used by the root binary
- **`model/`** — Core data models (`TestCaseDefinition`, expected signals)
- **`internal/legacyyaml/`** — legacy (v1/v2) test-case parsing, execution, and
  signal-specific assertions (`runner.go`, `traces.go`, `metrics.go`,
  `logs.go`, `profiles.go`); used only by `migrate`
- **`testhelpers/`** — Docker Compose management, Kubernetes (k3d), HTTP request helpers, response parsing
- **`observability/`** — Observability endpoint interface
- **`tests/`** — Integration and e2e test fixtures

### Test Case Schema

Current user-facing syntax is documented in `README.md`. Legacy yaml parsing
still exists in-package for migration support, but the repo's CLI surface is
the current `oats-config.yaml` + case-yaml flow.

## CLI Usage

```bash
# Print a plan
oats list

# With flags
oats --config oats-config.yaml --timeout 1m
```

Subcommands: `run` (default), `list`, `migrate`, `cache clear`, `version`.

Key flags: `--config`, `--tags`, `--timeout`, `--interval`, `--absent-timeout`,
`--parallel`, `--gcx`, `--gcx-version`, `--gcx-download`, `--gcx-context`

## Code Conventions

- Go 1.24+
- Assertions: gomega
- YAML parsing: `go.yaml.in/yaml/v3` with strict field validation
- Logging: `log/slog`
- Docker: `docker compose` (not legacy `docker-compose`)

## Testing

- Unit tests: `mise run test`
- Uses gomega for assertions, stretchr/testify for test utilities

## CI

- Lint on PRs (`mise run lint`), build on PRs (`mise run build`), tests on PRs (`mise run test`)
- E2E tests in a separate workflow
- Linting via flint

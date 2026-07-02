# OpenTelemetry Acceptance Tests (OATs)

OATs is a declarative acceptance-test framework for OpenTelemetry.

The current `oats` binary is the gcx-driven CLI. The legacy root runner has
been removed; existing users must migrate to the current `oats.toml` +
case-yaml flow when upgrading.

## Install

```sh
go install github.com/grafana/oats@latest
```

## Quick start

```sh
./scripts/build-local-tools.sh
bin/oats --config examples/smoke/oats.toml --list
bin/oats --config examples/smoke/oats.toml
```

## Key docs

- current syntax and feature status: [CURRENT.md](CURRENT.md)
- migration guidance: [UPGRADING.md](UPGRADING.md)
- small runnable examples: [`examples/smoke/`](examples/smoke/)
- richer fixture examples: [`examples/fixtures/`](examples/fixtures/)

## Current scope

- traces / logs / metrics / profiles via `gcx`
- structural collector-style `match_spans` / `match`
- app-backed and inline-OTLP seed modes
- remote / compose / k3d fixtures
- custom checks
- best-effort migration from legacy OATS yaml via:

  ```sh
  oats --migrate path/to/legacy.yaml
  ```

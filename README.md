# OpenTelemetry Acceptance Tests (OATs)

OATs is a declarative acceptance-test framework for OpenTelemetry. You describe,
in yaml, the telemetry an instrumented app *should* produce — traces, logs,
metrics, profiles — and OATS drives the app (or seeds telemetry directly), then
asserts against a real observability stack (Grafana, Loki, Tempo, Prometheus,
Pyroscope) via [`gcx`](https://github.com/grafana/gcx).

A case reads like the outcome you care about:

```yaml
oats-schema-version: 3
name: rolldice traces have a route attribute

fixture:
  compose:
    template: lgtm
    file: docker-compose.oats.yml

seed:
  type: app
input:
  - path: /rolldice?rolls=5

expected:
  traces:
    - traceql: '{ span.http.route = "/rolldice" }'
      match_spans:
        - name: "GET /rolldice"
```

## Install

1. Install [mise](https://mise.jdx.dev/).

2. Pin `oats` and `gcx` into your repo — mise resolves the latest releases and
   locks them into `mise.toml`:

   ```sh
   mise use --pin aqua:grafana/oats aqua:grafana/gcx
   ```

For CI, see [docs/ci.md](docs/ci.md).

Without mise:

- **GitHub releases** — download for your OS/arch from the
  [oats](https://github.com/grafana/oats/releases) and
  [gcx](https://github.com/grafana/gcx/releases) release pages.
- **go install** (oats from source) — `go install github.com/grafana/oats@latest`.

`oats` drives assertions through `gcx`; for fixture-backed runs OATS can
bootstrap gcx itself, but pinning it explicitly keeps runs reproducible.

## Quick start

```sh
# Print the run plan without executing
oats list --config examples/smoke/oats.toml

# Run it
oats --config examples/smoke/oats.toml
```

To hack on OATS itself (builds local `oats` + `gcx` into `./bin`):

```sh
./scripts/build-local-tools.sh
bin/oats version
```

## CLI

```sh
oats [flags]                     # run the suites (implicit; same as `oats run`)
oats run [flags]                 # run the suites
oats list --config oats.toml     # print the run plan and exit
oats migrate legacy.yaml         # convert one legacy yaml to the v3 shape
oats cache clear                 # delete all cached results
oats version                     # print the version
```

Common flags:

| Flag | Default | Meaning |
|------|---------|---------|
| `--config` | `oats.toml` | path to the config file |
| `--suite` | all | comma-separated suite names to run |
| `--tags` | all | comma-separated tags; a case runs if it matches any |
| `--timeout` | `30s` | per-assertion timeout — each assertion is retried until it passes or this elapses |
| `--interval` | `500ms` | polling interval between assertion retries |
| `--absent-timeout` | `10s` | window an `absent` assertion must stay empty |
| `--parallel` | `1` | suites to run concurrently, when fixture isolation allows |
| `--fail-fast` | `false` | stop scheduling further cases after the first case failure |
| `--no-cache` | `false` | disable the skip-when-unchanged cache for this run |
| `--format` | `text` | output format: `text` or `ndjson` |
| `--gcx` | `gcx` | path to the gcx binary |
| `--gcx-context` | derived | override the gcx context (otherwise derived from the fixture endpoint) |
| `-v` / `-vv` / `-vvv` | — | increase verbosity |

Run `oats --help` for the full list, including the inline-OTLP seed host/port
overrides.

## What it covers

- traces / logs / metrics / profiles, queried through `gcx`
- structural collector-style row matching (`match` / `match_spans`)
- app-backed and inline-OTLP seed modes
- remote / compose / k3d fixtures
- custom-check scripts
- best-effort migration from legacy OATS yaml (`oats migrate path/to/legacy.yaml`)

## Documentation

- **[docs/case-reference.md](docs/case-reference.md)** — the full case + config
  shape: fixtures, seed modes, the assertion vocabulary, custom checks
- **[docs/ci.md](docs/ci.md)** — installing and running OATS in CI, plus result
  caching and its caveats
- **[UPGRADING.md](UPGRADING.md)** — migrating older (schema-2) repos to v3
- **[AGENTS.md](AGENTS.md)** — for contributors and coding agents working *on*
  OATS (build, layout, conventions)
- `examples/smoke/`, `examples/fixtures/` — small runnable examples

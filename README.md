# OpenTelemetry Acceptance Tests (OATs)

OATs is a declarative acceptance-test framework for OpenTelemetry.

The `oats` binary is the gcx-driven CLI. Legacy direct-yaml invocation has been
removed; upgrades now use the current `oats.toml` + case-yaml flow.

## Install

```sh
go install github.com/grafana/oats@latest
```

## Quick start

```sh
# Build local dev binaries (oats + gcx) into ./bin
./scripts/build-local-tools.sh

# Print the CLI version
bin/oats version
bin/oats -version

# Print what would run
bin/oats --config examples/smoke/oats.toml --list

# Run
bin/oats --config examples/smoke/oats.toml
```

## Layout

- `examples/smoke/` — small runnable examples
- `examples/fixtures/` — richer compose / k3d fixture examples
- `UPGRADING.md` — migration notes for older repos

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

## CLI

```sh
oats --config oats.toml --list
oats --config oats.toml --suite smoke
oats --config oats.toml --tags traces,logs
oats --config oats.toml --gcx-context my-lgtm
oats --config oats.toml --no-cache
oats --config oats.toml --fail-fast
oats --format ndjson
oats -v=1
oats -v=2
oats -v=3
```

Key flags:

- `--config`
- `--suite`
- `--tags`
- `--timeout`
- `--interval`
- `--absent-timeout`
- `--parallel`
- `--fail-fast` — stop scheduling further cases after the first case failure
- `--gcx`
- `--gcx-context`
- `--version`

## Config shape

```toml
[meta]
version = 2

[[suite]]
cases = ["examples/smoke/cases/*.yaml"]

[cache]
ttl_days = 7
```

Case yaml:

```yaml
oats-schema-version: 3
name: rolldice traces have route attribute

fixture:
  type: compose
  template: lgtm
  compose_file: docker-compose.oats.yml

seed:
  type: app
input:
  - path: /rolldice?rolls=5

expected:
  traces:
    - traceql: '{ span.http.route = "/rolldice" }'
      match_spans:
        - name: "GET /rolldice"
  metrics:
    - promql: 'dice_lib_rolls_counter_total{service_name="dice-server"}'
      value: '>= 0'
  logs:
    - logql: '{service_name="dice-server"}'
      contains: Received request
  custom-checks:
    - script: ./verify.sh
```

## Seed

A case populates the stack before assertions run via one of two `seed.type`
modes:

```yaml
# App-backed: the suite fixture boots an instrumented app, and `input`
# requests drive it so it emits telemetry.
seed:
  type: app
input:
  - path: /rolldice?rolls=5   # method defaults to GET
```

```yaml
# Inline-OTLP: the case carries its own payload, pushed as OTLP/HTTP JSON.
# No app, no SDK. Declare only the signals you need.
seed:
  type: inline-otlp
  traces:
    - service: my-service
      spans:
        - name: seed-operation      # kind defaults to INTERNAL, duration to 200ms
  logs:
    - service: my-service
      body: seed-log-line           # severity_number defaults to 9 (INFO)
  metrics:
    - service: my-service
      name: seed_counter            # monotonic sum → PromQL `seed_counter_total`
      value: 42
```

> Inline-OTLP can seed traces, logs, and metrics. **Profiles cannot be
> inline-seeded** — assert profiles against an app-backed fixture that produces
> them (e.g. an eBPF profiler or a pyroscope-instrumented app).

## Assertions

Every signal block under `expected` shares one assertion vocabulary, plus a
few signal-specific keys. Each entry first names a query, then the checks that
must hold against its result.

Shared keys (valid on `traces`, `metrics`, `logs`, `profiles`):

| Key | Meaning |
|-----|---------|
| `contains` | string (or list) that must appear in the query output |
| `not_contains` | string (or list) that must **not** appear |
| `regex` | RE2 pattern (or list) that must match the output |
| `match` | structural row match — list of `{match_type, name, attributes}` |
| `count` | comparison against the number of rows, e.g. `'== 1'`, `'>= 2'` |
| `absent` | the query must return nothing for the whole `--absent-timeout` window |

`match` (and the trace-only `match_spans`) entries:

```yaml
match:
  - match_type: strict     # "strict" (default) or "regexp"
    name: seed-log-line    # for regexp, an RE2 pattern
    attributes:            # optional; list of {key, value?}
      - key: service_name
        value: my-service  # omit `value` to assert the key is merely present
```

Signal-specific keys:

- `traces`: `traceql` (required), `match_spans` (span-row match, same shape as `match`)
- `metrics`: `promql` (required), `value` (compare the sample value, e.g. `'>= 1'`, `'== 42'`)
- `logs`: `logql` (required)
- `profiles`: `query` (required)

Example covering several shapes:

```yaml
expected:
  traces:
    - traceql: '{ resource.service.name = "my-service" }'
      match_spans:
        - match_type: regexp
          name: '^GET /rolldice.*'
          attributes:
            - key: service.name
              value: my-service
  metrics:
    - promql: 'seed_counter_total{service_name="my-service"}'
      value: '>= 1'
  logs:
    - logql: '{service_name="my-service"}'
      regex: '.*rolling the dice.*'
      count: '>= 1'
  profiles:
    - query: 'process_cpu:cpu:nanoseconds:cpu:nanoseconds{service_name="my-service"}'
      contains: main
```

### compose-logs

For `compose` fixtures, `expected.compose-logs` greps the container logs
(`docker compose logs`) for each string — useful for asserting on output that
never reaches the OTLP pipeline:

```yaml
expected:
  compose-logs:
    - app started ok
```

## Custom checks

`expected.custom-checks` runs an arbitrary script and treats **exit code 0 as
pass**, non-zero as fail. Combined stdout+stderr is captured and printed on
failure. Like every assertion it is retried until it passes or `--timeout`
elapses.

```yaml
expected:
  custom-checks:
    - script: ./verify.sh      # resolved relative to the case file's directory
```

`script` may be a path (relative to the case dir, or absolute) or an inline
script beginning with a `#!` shebang. The process runs with its working
directory set to the case dir and inherits the parent environment plus these
OATS-provided variables:

| Variable | Fixtures | Meaning |
|----------|----------|---------|
| `OATS_FIXTURE_TYPE` | all | `remote` / `compose` / `k3d` |
| `OATS_GRAFANA_URL` | all | base URL of the fixture's Grafana |
| `OATS_APP_URL` | remote, k3d | base URL of the app under test |
| `OATS_OTLP_HTTP` | compose, k3d | OTLP/HTTP endpoint |
| `OATS_PYROSCOPE_URL` | compose, k3d | Pyroscope base URL |
| `COMPOSE_PROJECT_NAME`, `COMPOSE_FILE`, `OATS_COMPOSE_FILE_ARGS` | compose | let the script run its own `docker compose` commands |

A custom check that queries Grafana directly (replacing, for example, a legacy
`compose-logs` grep with a real LogQL query):

```bash
#!/usr/bin/env bash
set -euo pipefail
curl -fsS "${OATS_GRAFANA_URL:?}/api/health" >/dev/null
echo "custom check ok"
```

## Notes

- Cases inside one suite still run sequentially.
- Suites can run in parallel with `--parallel N` when fixture isolation allows
  it. Today that mainly means remote suites and compose suites where OATS owns
  the LGTM ports (`template = "lgtm"`).
- Case-local `fixture:` blocks cover the common one-case-per-suite shape.
- OATS owns local LGTM bootstrapping and gcx bootstrap for fixture-backed runs.

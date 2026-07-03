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
oats --format ndjson
oats -v
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

## Notes

- Cases inside one suite still run sequentially.
- Suites can run in parallel with `--parallel N` when fixture isolation allows
  it. Today that mainly means remote suites and compose suites where OATS owns
  the LGTM ports (`template = "lgtm"`).
- Case-local `fixture:` blocks cover the common one-case-per-suite shape.
- OATS owns local LGTM bootstrapping and gcx bootstrap for fixture-backed runs.

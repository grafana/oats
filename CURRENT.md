# Current OATS CLI

This document covers the current gcx-driven OATS CLI, which replaces the bespoke TraceQL /
PromQL / LogQL HTTP query infrastructure with the [gcx](https://github.com/grafana/gcx)
CLI. The current `oats` binary runs the gcx-driven implementation. See
the internal design doc (grafana/internal-docs#14)
for the full design.

## Quick start

```sh
# Build oats plus the gcx binary it shells out to
./scripts/build-local-tools.sh

# Print what would run, do not execute
bin/oats --config oats.toml --list

# Run against an already-running stack
bin/oats --config oats.toml --gcx-context my-lgtm

# Disable cache for this run
bin/oats --config oats.toml --no-cache

# Verbose output
bin/oats -v          # adds per-case PASS lines
bin/oats -v=2        # adds the gcx command behind each assertion
bin/oats -v=3        # adds fixture lifecycle and full gcx stdout

# Machine-readable
bin/oats --format ndjson > events.jsonl
```

The repo includes a small reference config under `examples/smoke/` showing:

- an app-backed case with `input`
- an inline-OTLP case
- a `custom-checks` case
- a profile assertion case

For richer fixture examples, `examples/fixtures/` shows:

- multi-file compose fixtures with env passthrough
- k3d fixture config with app build/import fields

## Current implemented scope

Today this branch already includes:

- gcx-driven traces / logs / metrics / profiles querying
- collector-style structural assertions (`match_spans` for traces, `match` elsewhere) with `match_type: strict | regexp`
- app-backed and inline-OTLP seed modes
- input-driving HTTP requests
- custom checks
- remote / compose / k3d fixture flows
- best-effort legacy migration with runnable migrated outputs for core cases
- text and NDJSON reporting, including fixture lifecycle events at verbose levels

## Architecture

```text
discovery → seed → engine → assert → report
                ↑
                fixture
                ↑
                wait (polling)
                ↑
                cache (skip-when-unchanged)
```

| Package    | Responsibility |
|------------|---------------|
| `discovery` | Parse `oats.toml`, expand case globs, apply filters, and derive |
|            | case-local fixtures when a suite omits one. |
| `casefile`  | Parse and validate one current-format case yaml file. |
| `seed`      | Push inline-OTLP payloads at an OTLP/HTTP endpoint. |
| `engine`    | Execute a gcx command, capture stdout/stderr/exit. |
| `signalcmd` | Translate a `casefile` assertion into gcx args. |
| `assert`    | The expectation vocabulary: `contains`, `not_contains`, `regex`, `value`, `count`, `absent`. |
| `wait`      | `Until` / `While` polling primitives (replaces gomega.Eventually). |
| `report`    | Compact-text and NDJSON renderers driven by an Event stream. |
| `cache`     | Skip-when-unchanged store keyed by |
|            | `(case yaml + fixture + gcx version + oats version)`. |
| `runner`    | Orchestrates a suite: seed → poll-and-assert → report, with optional cache. |
| `internal/cli` | The gcx-driven CLI implementation package used by the root `oats` binary. |

## Consumer-shape notes

For simple consumer repos, keep `oats.toml` thin: a suite usually only needs
`cases = ["..."]`. Case-local `fixture:` blocks now cover the common
one-case-per-suite path. Shared root-level `[fixture.*]` blocks are still useful
when many suites intentionally reuse the same fixture or when one case is run
against multiple fixtures.

Local LGTM compose boot plus Grafana auth bootstrap are now owned by OATS
itself: consumer repos should not need their own shared
`docker-compose.lgtm.yml` or a custom `gcx-wrapper.sh` just to talk to a local
LGTM stack.

## `oats.toml` shape

```toml
[meta]
version = 2

[[suite]]
cases = ["examples/nodejs/oats.yaml"]

# Optional when many suites share one fixture:
# [[suite]]
# name    = "lgtm-examples"
# cases   = ["examples/*/oats.yaml"]
# fixture = "lgtm-shared"
# tags    = ["traces", "metrics", "logs"]
#
# [fixture.lgtm-shared]
# type     = "compose"
# template = "lgtm"

[cache]
ttl_days = 7                   # default
```

## Case yaml shape

```yaml
oats: 2
name: rolldice traces have route attribute

fixture:
  type: compose
  template: lgtm
  compose_file: docker-compose.oats.yml

seed:
  type: app
  compose: docker-compose.app.yml   # optional legacy shorthand; suite fixture usually owns boot
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
      match:
        - attributes:
            - key: service_name
              value: dice-server
  logs:
    - logql: '{service_name="dice-server"} |~ `Received request`'
      match:
        - name: "Received request to roll dice"
          attributes:
            - key: service_name
              value: dice-server
  custom-checks:
    - script: ./verify.sh
```

For `seed.type: app`, the application is normally started by the suite fixture in
`oats.toml`. `seed.compose` is still accepted as a migration/compatibility hint,
but the runner does not require it when a fixture already provides the app.

Inline-OTLP seed (no example app required):

```yaml
oats: 2
name: gcx returns seeded trace

seed:
  type: inline-otlp
  traces:
    - service: gcx-e2e-seed
      spans:
        - name: seed-operation

expected:
  traces:
    - traceql: '{ resource.service.name = "gcx-e2e-seed" }'
      match_spans:
        - name: seed-operation
          attributes:
            - key: service.name
              value: gcx-e2e-seed
```

### Assertion keys

Per signal under `expected.<signal>[]`:

| Key | Meaning |
|-----|---------|
| `traceql` / `promql` / `logql` / `query` | The query string. |
| `contains` | Substrings that must appear in gcx stdout. Accepts a string or list of strings. |
| `not_contains` | Substrings that must not appear. Accepts a string or list of strings. |
| `regex` | Patterns that must match. Accepts a string or list of strings. |
| `match_spans` | Trace-only structural assertions over returned spans using collector-style `match_type: strict | regexp`. |
| `match` | Structural assertions for logs / metrics / profiles using collector-style `match_type: strict | regexp`. |
| `value` | Metrics only — numeric comparison (`>= 0`, `== 42`). |
| `count` | Comparison against the number of result rows. |
| `absent` | If true, the query must return zero rows. |

Per custom check under `expected.custom-checks[]`:

| Key | Meaning |
|-----|---------|
| `script` | Either an executable path resolved relative to the case yaml, or an inline shell script block. |

When a case declares `input`, the runner makes those HTTP requests before each
assertion poll, mirroring OATS v1's “drive the app until telemetry appears”
behavior. For remote fixtures, point those requests at a running app with
`--app-host` and `--app-port` (defaults: `localhost:8080`).

`match_spans` (for traces) and `match` (for logs / metrics / profiles)
default to `match_type: strict`. Attributes follow collector-style
`[{ key, value? }]` entries; omitting `value` means “the key must be present”.
Text assertions keep list semantics internally, but author-facing YAML may use
either a scalar string or a list of strings.

```yaml
match:
  - name: seed-operation
    attributes:
      - key: service.name
        value: gcx-e2e-seed
      - key: trace_id
  - match_type: regexp
    attributes:
      - key: http.route
        value: "^/roll.*$"
```

## What's not here yet

- Parallel cases within a suite.
- k3d pool warmup / reuse.
- Full-fidelity `oats migrate` for fixture/input semantics. A best-effort
  `oats --migrate <legacy.yaml>` now converts expectation blocks into the
  collector-style `match` schema and prints warnings for dropped/unsupported
  fields (multi-entry matrix cases still expand manually).
- Hermeticity static-check (runtime check applies).

## Migrating from v1

For the legacy → current migration story see the OATS implementation plan in
the internal design doc (grafana/internal-docs#14).
Today a best-effort converter exists:

```bash
oats --migrate path/to/oats.yaml > migrated.yaml
```

It converts legacy `equals` / `regexp` / `attributes` /
`attribute-regexp` assertions into structural matcher entries
(`match_spans:` for traces, `match:` elsewhere) and prints warnings for
fields that still need manual follow-up. For richer legacy fixtures, the
warnings now include ready-to-paste `oats.toml` fixture snippets for
multi-file compose/env and kubernetes→k3d mappings. Single-entry legacy
`matrix:` cases are flattened automatically; multi-entry matrix cases emit
fixture-expansion hints for manual splitting. The v1 binary (`oats`) and the
current binary (`oats`) still reads a different file shape than the legacy YAML runner.

## Verbosity contract

| Flag | Adds to stdout |
|------|----------------|
| (default) | Failures + final summary only. |
| `-v` | One line per passing case. |
| `-v=2` | The gcx invocation behind each assertion. |
| `-v=3` | Fixture lifecycle, full gcx stdout, per-phase timing. |

`--format ndjson` emits one JSON object per event regardless of verbosity.
Pass and fixture-lifecycle events are filtered at the same verbosity
thresholds. Under GitHub Actions, failures also emit `::error file=...,line=...::`
annotations for inline PR-diff visibility.

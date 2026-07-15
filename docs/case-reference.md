# Case reference

The full shape of an OATS project: the `oats-config.yaml` config, the case yaml, seed
modes, the assertion vocabulary, and custom checks. For a quick start and the
CLI summary see the [README](../README.md); for running in CI see
[ci.md](ci.md).

## oats-config.yaml

`oats-config.yaml` is the entry point of an OATS project: it declares `meta`, a
top-level `cases:` list of the case files to run (path globs), and cache
settings. It does **not** declare fixtures or any grouping — each fixture lives
on its case (see [Fixtures](#fixtures)), and OATS derives the boot-grouping from
those fixtures (see [How cases are grouped](#how-cases-are-grouped)). `oats`
looks for the config in the current directory and then each parent (so you can
run from a subdirectory); `--config <path>` overrides the search. Its
`meta.version` is the single OATS schema version — case files carry no version of
their own.

```yaml
meta:
  version: 3
cases: ["*/oats-case.yaml"]       # each case in its own subdir; globs are relative to this file
cache:
  ttl_days: 7                     # skip-when-unchanged TTL; 0 → default (7 days)
```

The common layout is one case per directory — a `oats-case.yaml` alongside the
files it needs (compose files, custom-check scripts) — collected with a glob like
`*/oats-case.yaml`:

```text
oats-config.yaml
go/oats-case.yaml       go/docker-compose.oats.yml
python/oats-case.yaml   python/docker-compose.oats.yml
```

Globs are shell-style (`*` matches one path segment; no recursive `**`), so add a
segment for deeper trees (e.g. `examples/*/oats-case.yaml`) or list several
patterns.

### How cases are grouped

You write **cases**; OATS derives the grouping. A case is one test, and each
case carries its own `fixture:` (see [Fixtures](#fixtures)). OATS groups cases
by **fixture identity**: cases with the same fixture form one **boot-group** —
the fixture boots once and those cases run serially against it — while cases with
different fixtures form independent groups that can run in parallel (where
fixture isolation allows; see [Running in parallel](#running-in-parallel)).
There is no grouping to declare. This is why, for example,
docker-otel-lgtm's per-language cases — each its own `compose` fixture — auto-
group into independent parallel boots with no grouping config.

For a `compose`/`k3d` fixture that shared boot is the real win, since the
(often expensive) backend is stood up once. A `remote` fixture boots nothing, so
grouping there only affects reporting.

Fixture identity has one subtlety: a fixture that resolves files relative to the
case dir (a `compose` `file`/`files`, or a `k3d` `k8s_dir` / `app_docker_file` /
`app_docker_context`) **additionally groups by directory** — the same relative
path means different files in different dirs, so such cases group only with
others in the same directory. A path-less fixture (a `remote` fixture, or a
template-only `compose` fixture) groups on the fixture alone, so identical copies
in different directories share one boot.

**Tags and filtering are per-case.** Put `tags: [...]` on the case
(`oats-case.yaml`), and `oats --tags <t>` filters cases (any match). If no case
declares a fixture, it defaults to a `compose` fixture with the builtin `lgtm`
template.

## Case yaml

```yaml
name: rolldice traces have route attribute

fixture:
  compose:
    template: lgtm
    file: docker-compose.oats.yml

input:                       # drive the app (seed defaults to type: app)
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

## Fixtures

A fixture describes **both the backend and the app under test, together** — the
observability stack (Grafana + Loki + Tempo + Prometheus + Pyroscope) plus, for
an app-seed case, the app that feeds it. It is declared **on the case**, in a
`fixture:` block (there is no config-level fixture); OATS boots each distinct
fixture once and runs the cases that share it against it (see [How cases are
grouped](#how-cases-are-grouped)). For a `compose` fixture the builtin `lgtm`
template supplies the backend and the case's `file:` supplies the app.

Because cases that share a fixture share one boot, they run against the same
app+backend, stood up once. (Not yet: sharing one backend across cases that run
*different* apps — the shared-LGTM parallel model — is a deliberately deferred
follow-up.)

Set exactly one of three nested blocks; the block you set selects how the stack
is stood up (there is no separate `type` field):

| Block | Meaning |
|-------|---------|
| `remote` | point at an already-running stack (`endpoint:` / a gcx context) |
| `compose` | OATS boots a docker-compose stack; `template` defaults to `lgtm`, booting a builtin grafana/otel-lgtm alongside your `file`/`files`. Set `template: none` to bring your own stack |
| `k3d` | OATS boots a k3d (k3s-in-docker) cluster |

A `compose` fixture with no `template` defaults to `template: lgtm`, so OATS
boots the builtin LGTM stack next to your `file`/`files`. Omit `file`/`files`
entirely (or drop the `fixture:` block altogether) to boot just the LGTM stack —
handy for `inline-otlp` smoke tests. To skip the builtin stack, set
`template: none` (then `file`/`files` are required).

```yaml
fixture:
  remote:
    endpoint: http://localhost:4318
---
fixture:
  compose:
    template: lgtm
    file: docker-compose.oats.yml   # or files: [a.yml, b.yml]
---
fixture:
  k3d:
    k8s_dir: k8s
    app_service: rolldice
    app_docker_file: Dockerfile
    app_port: 8080
```

`compose` and `k3d` fixtures are booted, waited on for readiness, and torn down
by OATS. `remote` fixtures are assumed ready.

For a `compose` fixture driven by a `seed: app` case, set `app_service` (the
compose service name of the app) and `app_port` (the app's container port) inside
the `compose` block. OATS then discovers the host port docker published for that
service, so the app can bind an **ephemeral** host port (`127.0.0.1::<app_port>`)
rather than a fixed one — which is what lets app-seed groups run under `--parallel`
without colliding.
Omit them and the app is driven on the fixed `--app-port` (default 8080), which
forces the group to run serially.

## Seed

A case populates the stack before assertions run via one of two `seed.type`
modes:

- **`app`** is the default (omit `seed` entirely): drive your real instrumented
  app and assert on what it actually emits end-to-end (SDK → collector →
  backend). This is what most consumer repos want.
- **`inline-otlp`** carries a hand-written OTLP payload and pushes it straight
  at the OTLP endpoint — no app, no SDK. Reach for it when there is no app to
  drive: to exercise the pipeline or backend in isolation (collector
  processor/transform config, ingestion, query behaviour), or to pin an exact
  payload shape a test depends on. OATS's own e2e tests lean on it heavily for
  precisely this — deterministic telemetry without booting an instrumented app.

```yaml
# App-backed (the default — no seed block needed): the case's fixture boots an
# instrumented app, and `input` requests drive it so it emits telemetry.
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
must hold against its result. Every assertion is retried until it passes or
`--timeout` elapses.

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

## Running in parallel

- Cases inside one fixture group run sequentially.
- Fixture groups can run concurrently with `--parallel N`, but only where fixture
  isolation allows it: remote groups, and `template = "lgtm"` compose groups that
  publish **no fixed host ports**. An app-seed compose group qualifies when its
  app binds an *ephemeral* host port (`127.0.0.1::<port>`) and the fixture sets
  `app_service` + `app_port` so OATS can discover the published port; an app-seed
  group without `app_service`, or any compose file that binds a fixed host port,
  runs serially. k3d groups always run serially.
- **Memory, not CPU, is the limit for compose parallelism.** Each parallel
  `template = "lgtm"` group boots its *own* LGTM stack — a unique compose project
  with dynamically allocated host ports — so groups can neither collide on ports
  nor see each other's telemetry. That hermetic isolation costs one full LGTM
  container per concurrent group (Grafana + Loki + Tempo + Mimir + Prometheus +
  Pyroscope + collector, on the order of ~1 GB each). `--parallel` therefore
  defaults to `1`; raise it deliberately, sized to available RAM rather than core
  count. Remote groups boot nothing and parallelize cheaply.
- OATS owns local LGTM bootstrapping and gcx bootstrap for fixture-backed runs.

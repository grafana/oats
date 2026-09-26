---
name: oats
description: >-
  Author, run, and diagnose OpenTelemetry acceptance tests with the OATs CLI.
  Use for validating application telemetry or collector/backend pipelines with
  traces, logs, metrics, and profiles. Not for OATs repository maintenance.
---

# OpenTelemetry acceptance tests

Use the installed `oats --help`, `oats run --help`, and `oats usage` to discover
that version's CLI. OATs queries observability backends through the separate
`gcx` executable; it is not bundled into `oats`.

## Choose the test boundary

- To test instrumentation end to end, drive the real application with `input`
  and leave `seed` unset (the default is `app`). Do not replace an application
  test with synthetic telemetry just to make it pass.
- Use `seed.type: inline-otlp` for a collector/backend pipeline test that needs
  a controlled trace, log, or metric payload. Profiles require a real producer.
- Prefer the project's existing fixture. Compose and k3d fixtures start and
  stop infrastructure; a remote fixture assumes the backend is already ready.
  Confirm the target before sending telemetry or requests to a remote system.

## Author the current format

`oats-config.yaml` contains `meta.version: 3`, case paths, and optionally cache
settings. Fixtures and tags belong on each case, not the config. Cases have no
version field. Paths in `cases` are relative to the config; globs are
shell-style, not recursive `**` patterns.

A small **pipeline smoke test**, not an instrumentation test:

```yaml
# oats-config.yaml
meta:
  version: 3
cases: [smoke/oats-case.yaml]
```

```yaml
# smoke/oats-case.yaml
name: trace ingestion smoke
fixture:
  compose:
    template: lgtm
seed:
  type: inline-otlp
  traces:
    - service: oats-smoke
      spans:
        - name: smoke-operation
expected:
  traces:
    - traceql: '{ resource.service.name = "oats-smoke" }'
      match_spans:
        - name: smoke-operation
```

For application-backed Compose cases, keep the app's compose file next to the
case and set `fixture.compose.file`, `app_service`, and `app_port`. Use
`docker compose`, not `docker-compose`. Ephemeral host ports let isolated
fixture groups run concurrently. Cases sharing a fixture group run serially.

Scope queries to the telemetry under test. Traces use TraceQL, logs LogQL,
metrics PromQL, and profiles Pyroscope selectors. Prefer assertions on the
observable contract over unstable IDs or exact timing. Consult the
[development case reference](https://github.com/grafana/oats/blob/main/docs/case-reference.md)
for additional assertion and fixture syntax. That link follows `main`, not the
installed release. For version-matched guidance, run `oats version` and select
the corresponding release tag in GitHub before using the reference.

## Run and diagnose

1. Run `oats list --config oats-config.yaml` to inspect discovery without
   starting fixtures. This does not prove the backend or assertions work.
2. Run the smallest relevant case selection, for example
   `oats --config oats-config.yaml smoke/ --no-cache -v`.
   A positional path normally filters cases; only when config discovery fails
   does one positional file/directory select the config/project instead.
3. For failures, increase verbosity: `-v` shows passes, `-vv` commands, and
   `-vvv` lifecycle details. Use `--format ndjson` for machine-readable events.
4. Distinguish discovery/schema errors, fixture readiness, input/OTLP delivery,
   gcx connection/query failures, and failed assertions before changing tests.
   Assertion polling repeats queries, not the input requests.
5. Use `--no-cache` when checking fixture or instrumentation changes: the cache
   does not hash fixture contents. Do not increase timeouts or weaken assertions
   before identifying the reason for the failure.

Command-line flags override their `OATS_*` environment equivalents. For
controlled/offline runs, provide gcx explicitly and use `--gcx-download never`;
otherwise OATs may download a checksum-verified gcx fallback. Backend containers
may also require image downloads.

Exit status is 0 for success, 1 for failed cases, and 2 for command/runtime
errors. Report what ran, whether cache was bypassed, and any infrastructure
blockers rather than treating an unexecuted test as passing.

`oats migrate <file>` prints a converted legacy case, while a directory argument
rewrites cases in place and creates a config. Review those mutations before
using directory migration on existing work.

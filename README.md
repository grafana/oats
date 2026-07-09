<!-- markdownlint-disable MD033 MD041 -->
<p align="center">
  <img src="assets/icon.svg" width="128" height="128" alt="oats logo">
</p>

<h1 align="center">OpenTelemetry Acceptance Tests (OATs)</h1>

<p align="center">
  <a href="https://github.com/grafana/oats/actions/workflows/lint.yml"><img src="https://github.com/grafana/oats/actions/workflows/lint.yml/badge.svg" alt="Lint"></a>
  <a href="https://github.com/grafana/oats/releases"><img src="https://img.shields.io/github/v/release/grafana/oats" alt="GitHub Release"></a>
</p>

<p align="center">
  <a href="docs/cli.md">CLI</a> ·
  <a href="docs/case-reference.md">Test Case Syntax</a> ·
  <a href="docs/ci.md">CI</a> ·
  <a href="UPGRADING.md">Upgrading</a>
</p>
<!-- markdownlint-enable MD033 MD041 -->

OATs is a declarative acceptance-test framework for OpenTelemetry. You describe,
in yaml, the telemetry an instrumented app *should* produce — traces, logs,
metrics, profiles — and OATS drives the app (or seeds telemetry directly), then
asserts against a real observability stack (Grafana, Loki, Tempo, Prometheus,
Pyroscope) via [`gcx`](https://github.com/grafana/gcx).

A case reads like the outcome you care about:

```yaml
name: rolldice traces have a route attribute

fixture:
  compose:
    file: docker-compose.oats.yml   # your app; OATS adds a Grafana LGTM stack by default

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

## Getting started

The best starting point is **[`examples/python/`](examples/python/)** — a real,
instrumented Flask "rolldice" app tested end to end. With Docker running, copy it
and run:

```sh
cp -r examples/python my-oats-test && cd my-oats-test
oats   # run it
```

An OATS project is two files. **`oats-config.yaml`** lists the cases to run;
`oats` finds it in the current directory or any parent:

```yaml
meta:
  version: 3
cases: ["oats-case.yaml"]
```

**`oats-case.yaml`** is the test itself — the app to drive and the telemetry to
expect:

```yaml
name: python rolldice
fixture:
  compose:
    file: docker-compose.oats.yml   # your app; OATS adds a Grafana LGTM stack by default
    app_service: python             # so OATS can reach the app…
    app_port: 8082                  # …on its container port
seed:
  type: app
input:
  - path: /rolldice                 # drive the app so it emits telemetry
expected:
  traces:
    - traceql: '{ span.http.route = "/rolldice" }'
      match_spans:
        - name: GET /rolldice
  metrics:
    - promql: 'http_server_active_requests{http_method="GET"}'
      value: '>= 0'
  logs:
    - logql: '{service_name="rolldice"} |~ `rolling the dice`'
      regex: 'rolling the dice'
```

`oats` boots the app next to a throwaway Grafana LGTM stack (the default fixture —
no `template: lgtm` needed), drives `/rolldice`, and retries each assertion until
it passes or times out.

No app of your own yet? A case can seed telemetry directly with
`seed: {type: inline-otlp}` and skip the app entirely — see
[docs/case-reference.md](docs/case-reference.md).

## Examples

- **[`examples/python/`](examples/python/)** — flagship: a real Flask app +
  compose fixture, asserting traces, metrics, and logs. **Start here.**
- **[`examples/smoke/`](examples/smoke/)** — advanced: a remote fixture with
  inline-otlp / app / profile / custom-check cases.
- **[`examples/fixtures/`](examples/fixtures/)** — advanced: compose and k3d
  fixtures side by side.

## Documentation

- **[docs/cli.md](docs/cli.md)** — every command and flag
- **[docs/case-reference.md](docs/case-reference.md)** — test case syntax: the
  full config + case shape (fixtures, seed modes, assertion vocabulary, custom checks)
- **[docs/ci.md](docs/ci.md)** — installing and running OATS in CI, plus result
  caching and its caveats
- **[UPGRADING.md](UPGRADING.md)** — migrating older (schema-2) repos to v3
- **[AGENTS.md](AGENTS.md)** — for contributors and coding agents working *on*
  OATS (build, layout, conventions)

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
  <a href="docs/case-reference.md">Case reference</a> ·
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

## Getting started

An OATS project is a directory with an **`oats-config.yaml`** plus one case per
subdirectory. The smallest project that runs end-to-end is two files and Docker —
OATS boots a throwaway Grafana LGTM stack for you, so you need no running backend
and no app to see it work.

**`oats-config.yaml`** — lists the cases:

```yaml
meta:
  version: 3
cases: ["*/oats-case.yaml"]   # one case per subdirectory
```

**`hello/oats-case.yaml`** — boot an LGTM stack, push a log, assert it lands:

```yaml
name: hello world
fixture:
  compose:
    template: lgtm          # OATS boots grafana/otel-lgtm; no compose file needed
seed:
  type: inline-otlp         # push telemetry directly — no instrumented app required
  logs:
    - service: hello
      body: hello from oats
expected:
  logs:
    - logql: '{service_name="hello"}'
      contains: hello from oats
```

Then, from the project directory:

```sh
oats list   # show the resolved plan
oats        # boot the fixture, seed it, assert
```

To test a **real app** instead of inline telemetry, point the fixture at your
app's compose file and use `seed: {type: app}` with `input:` requests. The
runnable **[`examples/`](examples/)** projects are the best starting point to
copy from — `examples/smoke/` (remote fixture + assertions) and
`examples/fixtures/` (compose and k3d) — see [docs/case-reference.md](docs/case-reference.md)
for the full shape.

## Documentation

- **[docs/cli.md](docs/cli.md)** — every command and flag
- **[docs/case-reference.md](docs/case-reference.md)** — the full config + case
  shape: fixtures, seed modes, the assertion vocabulary, custom checks
- **[docs/ci.md](docs/ci.md)** — installing and running OATS in CI, plus result
  caching and its caveats
- **[UPGRADING.md](UPGRADING.md)** — migrating older (schema-2) repos to v3
- **[AGENTS.md](AGENTS.md)** — for contributors and coding agents working *on*
  OATS (build, layout, conventions)
- **[`examples/`](examples/)** — small runnable projects to copy from

# Running OATS in CI

OATS is two binaries: `oats` (the runner) and `gcx` (the assertion engine it
drives). This guide covers installing both reproducibly and wiring a fast,
correct CI job. The reference model is
[opentelemetry-java-examples](https://github.com/open-telemetry/opentelemetry-java-examples/blob/main/.github/workflows/oats-tests.yml),
which already gates by path and runs everything through mise.

## Install

### mise (recommended)

Pin both tools with the [aqua](https://mise.jdx.dev/dev-tools/backends/aqua.html)
backend — mise resolves the latest releases and locks them into `mise.toml`:

```sh
mise use --pin aqua:grafana/oats aqua:grafana/gcx
```

which produces a pinned `mise.toml`:

```toml
[tools]
"aqua:grafana/oats" = "0.7.0"
"aqua:grafana/gcx"  = "0.4.3"
```

Pinning is what makes CI caching safe (see below): a pinned release is an
immutable artifact, so `gcx --version` is byte-stable across runs and a version
bump is an explicit `mise.toml` diff. Renovate keeps the pins current.

### GitHub releases (no mise)

Download the binary for your OS/arch:

- oats — <https://github.com/grafana/oats/releases>
- gcx — <https://github.com/grafana/gcx/releases>

Pin to a specific tag; avoid tracking a floating "latest" if you rely on the
result cache.

### go install (oats from source)

```sh
go install github.com/grafana/oats@latest
```

`oats --gcx-version 0.4.3` can download and cache gcx itself for fixture-backed
runs. Release and mise-built oats binaries also embed the repository's pinned
gcx version and can download it automatically when the default `gcx` command is
missing. Set `OATS_GCX_DOWNLOAD=never` in strict or air-gapped CI; mise-managed
environments default to this policy. Pinning gcx explicitly with mise or a
release download remains useful when you want the tool installation visible in
your repository.

## The workflow shape

```yaml
on:
  pull_request:
    paths:                       # coarse gate: skip the whole job when nothing relevant changed
      - .github/workflows/oats-tests.yml
      - mise.toml
      - "my-example/**"
  workflow_dispatch:

jobs:
  oats:
    runs-on: ubuntu-24.04
    permissions:
      contents: read
    steps:
      - uses: actions/checkout@v7
      - uses: jdx/mise-action@v4   # installs pinned oats + gcx from mise.toml
      - run: oats   # finds oats-config.yaml in the repo root
```

The `paths` filter is the cheapest and most important optimization: if a PR
touches nothing the tests depend on, the job never runs. This alone covers most
of the saving.

## Result caching (optional)

`oats` keeps a skip-when-unchanged cache under `--cache-dir`. A case is skipped
when `(case yaml, fixture config, gcx version, oats version)` hashes to a
previous green run within the TTL. Within a job that *did* trigger, this skips
the cases a PR didn't actually affect.

To persist it across CI runs:

```yaml
      - uses: actions/cache@v6
        with:
          path: ~/.cache/oats
          key: oats-${{ hashFiles('mise.toml') }}
      - run: oats --cache-dir ~/.cache/oats
```

Keying on `hashFiles('mise.toml')` scopes the cache to a gcx/oats version pair,
so a version bump starts a fresh cache — no stale skips from version drift.

### Correctness caveat — floating image tags

The cache key hashes the fixture **config** (the `compose`/`k3d`/`remote` block —
compose file paths, image tags, env, endpoint ...), **not** the fixture's
**contents**. If the app
under test is baked into an image with a floating tag (`:latest`), rebuilding
that image under the same tag produces an identical key — so a case can be
skipped against stale bytes (a false green).

You are safe when either holds:

- the app is **built fresh from source in the same PR** and the case yaml lives
  in the changed directory (the path gate + case hash already cover it — this is
  the java-examples case), or
- the image is **pinned by digest** (`image@sha256:...`), so a new image changes
  the fixture config and therefore the key.

If neither holds, don't persist the cache in CI until the image digest is folded
into the cache key (`cache.Key.Extra` exists for exactly this; the CLI does not
set it today).

Caching is opt-in — a clear pin + path gate is the baseline recommendation, and
the cache is a bonus on top.

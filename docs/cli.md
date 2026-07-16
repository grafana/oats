# CLI reference

`oats` runs the cases declared by an `oats-config.yaml`. For the config and case
shapes see [case-reference.md](case-reference.md); for CI see [ci.md](ci.md).

## Config discovery

Every command that needs the config looks for **`oats-config.yaml`** in the
current directory and then each parent directory (like `git`), so you can run
`oats` from anywhere inside a project. Pass `--config <path>` to use a specific
file instead.

## Commands

### `oats [paths...]` / `oats run [paths...]`

Run the cases. Bare `oats` is an implicit `run`; the explicit `run` subcommand is
identical. With no positional args every case in the config runs; positional
**paths** (files or directories) restrict the run to cases at or under them —
they scope *which* cases run, they do not change where the config loads from.

```sh
oats                              # run every case
oats payments/                    # only cases under payments/
oats payments/checkout/           # only cases under payments/checkout/
oats --suite smoke --tags traces  # filter by suite name and tag
oats --parallel 4                 # run parallel-safe suites concurrently
```

A run boots each suite's fixture, seeds it, then polls every assertion until it
passes or `--timeout` elapses. Exit code is non-zero if any case fails.

Flags:

| Flag | Default | Meaning |
|------|---------|---------|
| `--config` | `oats-config.yaml`, searched from current working directory upward | config file to load |
| `--suite` | all | comma-separated suite names to run |
| `--tags` | all | comma-separated tags; a case runs if it matches any |
| `--parallel` | `1` | suites to run concurrently, where fixture isolation allows |
| `--fail-fast` | `false` | stop scheduling further cases after the first failure |
| `--timeout` | `30s` | per-assertion timeout — each assertion is retried until it passes or this elapses |
| `--interval` | `500ms` | polling interval between assertion retries |
| `--absent-timeout` | `10s` | window an `absent` assertion must stay empty to pass |
| `--seed-settle` | `2s` | wait after seeding before the first assertion |
| `--no-cache` | `false` | ignore the skip-when-unchanged cache for this run |
| `--cache-dir` | platform user cache/state directory + `/oats` | directory for the skip-when-unchanged cache |
| `--format` | `text` | output format: `text` or `ndjson` |
| `--gcx` | `gcx` | path to the gcx binary (`PATH`-resolved if a bare name) |
| `--gcx-version` | — | download and use this gcx release (for example, `0.4.3`) |
| `--gcx-context` | derived | gcx context to query (otherwise derived from the fixture endpoint) |
| `--app-host` / `--app-port` | `localhost` / `8080` | where to drive `input` requests when a fixture doesn't resolve the app endpoint itself |
| `--otlp-http` | `http://localhost:4318` | OTLP/HTTP base URL for the `inline-otlp` seed |
| `-v` / `-vv` / `-vvv` | — | increasing verbosity (passes / commands / lifecycle) |

### `oats list`

Print the resolved run plan — the suites, their fixtures, and the cases each
expands to — and exit without executing anything. Honors `--config` and the same
discovery. Useful to confirm globs and fixtures before a run.

### `oats migrate <path>`

Convert legacy (schema-2) OATS yaml to the v3 shape.

- **File** → prints one self-contained v3 case (including its `fixture:` block)
  to stdout; warnings about lossy/dropped fields go to stderr.
- **Directory** → migrates every legacy case found under it *in place* (each file
  is rewritten as its v3 equivalent, honoring `.oatsignore` and skipping
  templates) and writes an `oats-config.yaml` listing them explicitly. A summary
  and per-file warnings go to stderr; nothing to stdout.

```sh
oats migrate ./old-case.yaml > new-case.yaml   # one file
oats migrate .                                 # whole tree, in place
```

Legacy docker-compose/kubernetes fixtures become case-local `fixture:` blocks.
Migration is best-effort: review the warnings (e.g. multi-entry matrices and
`compose-logs` are not auto-converted).

### `oats cache clear`

Delete all cached results under `--cache-dir` (default: the platform user
cache/state directory plus `/oats`; `XDG_STATE_HOME` wins on Unix).
The cache lets a re-run skip cases whose `(case, fixture, gcx version, oats
version)` are unchanged and previously passed; clear it to force a full run.

### `oats version`

Print the oats version and exit.

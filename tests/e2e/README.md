# End-to-end cases

This directory uses a Flint-style case layout:

```text
tests/e2e/cases/<group>/<case>/
  test.yaml
  files/
    ...
```

## Defaults

- `files/oats-config.yaml` is the default OATS config (fixture-group/case list).
- When `files/oats-config.yaml` is absent, the test runner synthesizes one that points
  at `files/oats-case.yaml`.
- The harness boots a shared local LGTM stack plus real `oats` and real `gcx`.
- The default run command is:

  ```bash
  <built oats> --config .generated-oats.yaml --gcx <built gcx> --gcx-context local --no-cache --timeout 10s --interval 1s
  ```

- Expected exit code defaults to `0`.
- Cases run in parallel by default. Set `execution.mode: serial` to opt out.

## Fixture coverage note

The matrix includes explicit remote, compose (`compose-logs`), and k3d cases.
Pure matcher edge cases that proved flaky or not meaningfully end-to-end stay in
unit tests instead of this directory.

## Placeholder expansion

The runner expands these placeholders inside `test.yaml`, `files/*`, env values,
and shell commands:

- `{{REPO_ROOT}}`
- `{{CASE_DIR}}`
- `{{CASE_NAME}}`
- `{{REMOTE_OTLP_HTTP}}`
- `{{GCX}}`
- `{{GCX_CONFIG}}`
- `{{OATS}}`

## Filtering

Run the e2e suite through the mise task:

```bash
mise run e2e-test
```

Use `OATS_E2E_FILTER` to run a subset of cases by relative path:

```bash
OATS_E2E_FILTER=assert/ mise run e2e-test
OATS_E2E_FILTER=fixture/remote mise run e2e-test
OATS_E2E_FILTER=assert/contains,assert/regex mise run e2e-test
```

For direct `go test` runs, mise must be installed and its tools must be
available; alternatively, set `OATS_E2E_BIN_DIR` to a directory containing
prebuilt `oats` and `gcx` binaries.

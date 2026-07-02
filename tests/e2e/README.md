# End-to-end cases

This directory uses a Flint-style case layout:

```text
tests/e2e/cases/<group>/<case>/
  test.yaml
  files/
    ...
```

## Defaults

- `files/oats.yaml` is the default OATS input.
- When `files/oats.toml` is absent, the test runner synthesizes one that points
  at `files/oats.yaml`.
- The harness boots a shared local LGTM stack plus real `oats` and real `gcx`.
- The default run command is:

  ```bash
  <built oats> --config .generated-oats.toml --gcx <built gcx> --gcx-context local --no-cache --timeout 10s --interval 1s
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

Use `OATS_E2E_FILTER` to run a subset of cases by relative path:

```bash
OATS_E2E_FILTER=assert/ go test ./tests/e2e -run TestCases
OATS_E2E_FILTER=fixture/remote go test ./tests/e2e -run TestCases
OATS_E2E_FILTER=assert/contains,assert/regex go test ./tests/e2e -run TestCases
```

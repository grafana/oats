# Upgrading

The changelog is available in the [releases section](https://github.com/grafana/oats/releases).

This file only contains upgrade notes for breaking changes that require you to
modify your existing YAML test files.

## Unreleased / next major

⚠️ Breaking Changes — legacy root runner removed

The root `oats` binary now runs the gcx-driven current CLI only.

That means upgrades now require migrating to the current:

- `oats.toml` suite/discovery file
- current case yaml shape documented in [README.md](README.md)

The legacy “pass one or more old yaml files directly to `oats`” runner has been
removed. For one-off help migrating old cases, use:

```sh
oats migrate path/to/legacy.yaml
```

### Case schema: `oats-schema-version: 2` → `3`

The case-yaml assertion shape changed with the gcx-driven runner. Bump the tag
to `3` and update assertions. The `0.5.0` / `0.6.0` notes further down describe
the **version-2** shape (`equals`, `attribute-regexp`, `flamebearers`,
`regexp`); the mappings below take you from that shape to version 3.

**Logs — regex.** Version 2's `regexp:` becomes `regex:` (scalar or list).
`contains` / `not_contains` are also first-class again:

```diff
 logs:
   - logql: '{job="app"}'
-    regexp: "error"
+    regex: "error"
```

**Traces — span matching.** Version 2's flat `equals` / `attributes` /
`attribute-regexp` become structured `match_spans` entries with an explicit
`match_type` and a list-of-`{key, value}` attribute form:

```diff
 traces:
   - traceql: '{}'
-    equals: "GET /api"
-    attributes:
-      http.method: "GET"
-    attribute-regexp:
-      http.route: "/api/.*"
+    match_spans:
+      - match_type: strict
+        name: "GET /api"
+        attributes:
+          - key: http.method
+            value: "GET"
+      - match_type: regexp
+        attributes:
+          - key: http.route
+            value: "/api/.*"
```

A span name matched by regex:

```diff
 traces:
   - traceql: '{}'
-    regexp: "GET /api.*"
+    match_spans:
+      - match_type: regexp
+        name: "GET /api.*"
```

**Profiles.** Version 2's `flamebearers:` block becomes the shared assertion
vocabulary keyed off `query`:

```diff
 profiles:
   - query: 'process_cpu:cpu:nanoseconds:cpu:nanoseconds'
-    flamebearers:
-      equals: "my-function"
+    match:
+      - match_type: strict
+        name: "my-function"
```

**`compose-logs`.** Still supported natively for `compose` fixtures
(`expected.compose-logs: [ ... ]`). `oats migrate` does **not** convert it (it
emits a warning); either keep it as-is on a compose fixture, or replace it with
a `custom-checks` script that queries the backend directly — see the
[custom checks](README.md#custom-checks) contract.

**`matrix`.** Not migrated automatically. `oats migrate` flattens a single-entry
matrix and otherwise emits a hint; multi-entry matrices must be split into
separate cases by hand.

### Worked example: a full legacy case → v3

A legacy (schema-version 2) case plus its `docker-compose`:

```yaml
# oats.yaml (v2)
oats-schema-version: 2
docker-compose:
  files:
    - ./docker-compose.yaml
input:
  - path: /rolldice
expected:
  traces:
    - traceql: '{}'
      spans:
        - name: "GET /rolldice"
          attributes:
            http.request.method: "GET"
  logs:
    - logql: '{service_name="rolldice"}'
      contains: ["rolling the dice"]
```

Run `oats migrate ./oats.yaml`. Split the result into an `oats.toml` and a v3
case (the migrator prints both the case yaml and a suggested `[fixture]` block):

```toml
# oats.toml
[meta]
version = 2

[[suite]]
name    = "rolldice"
cases   = ["cases/*.yaml"]
fixture = "compose-lgtm"

[fixture.compose-lgtm]
type         = "compose"
compose_file = "docker-compose.yaml"
```

```yaml
# cases/rolldice.yaml (v3)
oats-schema-version: 3
name: rolldice
seed:
  type: app
input:
  - path: /rolldice
expected:
  traces:
    - traceql: '{}'
      match_spans:
        - match_type: strict
          name: "GET /rolldice"
          attributes:
            - key: http.request.method
              value: "GET"
  logs:
    - logql: '{service_name="rolldice"}'
      contains: "rolling the dice"
```

See [README.md](README.md) for the full version-3 assertion reference.

## 0.6.0

⚠️ Breaking Changes - Migration Required: File Version Tag

This release introduces explicit versioning for OATS test files.
All test files must now include an `oats-schema-version` tag.

Full release notes: <https://github.com/grafana/oats/releases/tag/v0.6.0>

### Add `oats-schema-version: 2` to all test files

All OATS test files must now include the `oats-schema-version` field at the
top level. In `v0.6.0` the required version was `2`.

```yaml
# ✅ Required in all test files
oats-schema-version: 2

docker-compose:
  files:
    - ./docker-compose.yaml
expected:
  metrics:
    - promql: "uptime_seconds_total{}"
      value: ">= 0"
```

> [!TIP]
> **Why version "2"?** Version "1" represents the old format without an
> explicit version tag. The version number will only be incremented when
> migrations are needed.

### File Discovery Changes

**Before:** OATS would discover all `*oats*.yaml` files (except those ending in
`-template.yaml`) in the specified directory.

**Now:** OATS only considers files with a `.yaml` or `.yml` extension that
contain the `oats-schema-version` tag.

### New: `oats-template` Flag

Template files that should be included (via `include:`) but not run as entry
points must now use the `oats-template: true` flag:

```yaml
# Template file (e.g., oats-base.yaml)
oats-schema-version: 2
oats-template: true

docker-compose:
  files:
    - ./docker-compose.yaml
```

This replaces the old naming convention of `*-template.yaml`.

### New: Pass Specific Files Instead of Directory

You can now pass specific test files instead of a directory for better performance:

```sh
# Run specific test files
oats /path/to/repo/test1.yaml /path/to/repo/test2.yaml

# Old and still valid way (will scan all yaml/yml files)
oats /path/to/repo
```

This is particularly useful when you have many YAML files in your repository
and want to avoid parsing all of them.

### Migration Steps

1. Add `oats-schema-version: 2` to all your test files
2. Add `oats-template: true` to any template files (files that are included but
   not entry points)
3. (Optional) Consider passing specific file paths instead of directories for
   better performance

## 0.5.0

⚠️ Breaking Changes — Changes Required to Your YAML Files

This release enforces stricter validation and **removes support for deprecated
YAML syntax**.
You must update your test files when upgrading.

> [!WARNING]
> Any unknown field in your YAML files will now cause validation errors instead
> of being ignored.

Full release notes: <https://github.com/grafana/oats/releases/tag/v0.5.0>

### 1. Replace `contains` with `regexp` in `logs` assertions

```yaml
# ❌ Old (no longer works)
logs:
  - logql: '{job="app"}'
    contains: ["error"]

# ✅ New
logs:
  - logql: '{job="app"}'
    regexp: "error"
```

```diff
logs:
  - logql: '{job="app"}'
-   contains: ["error"]
+   regexp: "error"
```

### 2. Remove `spans` array from traces

```yaml
# ❌ Old (no longer works)
traces:
  - traceql: '{}'
    spans:
      - name: "GET /api"
        allow-duplicates: true # duplicate span names are now always allowed
        attributes:
          http.method: "GET"
          http.route: "regex:/api/.*"

# ✅ New
traces:
  - traceql: '{}'
    equals: "GET /api"
    attributes:
      http.method: "GET"
    attribute-regexp:
      http.route: "/api/.*"
```

```diff
traces:
  - traceql: '{}'
-   spans:
-     - name: "GET /api"
-       allow-duplicates: true # duplicate span names are now always allowed
-       attributes:
-         http.method: "GET"
-         http.route: "regex:/api/.*"
+   equals: "GET /api"
+   attributes:
+     http.method: "GET"
+   attribute-regexp:
+     http.route: "/api/.*"
```

Span name with a regular expression:

```yaml
# ❌ Old (no longer works)
traces:
  - traceql: '{}'
    spans:
      - name: "regex:GET /api.*"

# ✅ New with regexp
traces:
  - traceql: '{}'
    regexp: "GET /api.*"
```

```diff
traces:
  - traceql: '{}'
-   spans:
-     - name: "regex:GET /api.*"
+   regexp: "GET /api.*"
```

### 3. Update profile flamebearers

```yaml
# ❌ Old (no longer works)
profiles:
  - query: 'process_cpu:cpu:nanoseconds:cpu:nanoseconds'
    flamebearers:
      contains: "my-function"

# ✅ New
profiles:
  - query: 'process_cpu:cpu:nanoseconds:cpu:nanoseconds'
    flamebearers:
      equals: "my-function"
```

```diff
profiles:
  - query: 'process_cpu:cpu:nanoseconds:cpu:nanoseconds'
    flamebearers:
-     contains: "my-function"
+     equals: "my-function"
```

---

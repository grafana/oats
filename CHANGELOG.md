# Upgrading

The changelog is available in the [releases section](https://github.com/grafana/oats/releases).

This file only contains upgrade notes for breaking changes that require you to modify your existing 
YAML test files.

## 0.6.0

⚠️ Breaking Changes - Migration Required: File Version Tag

This release introduces explicit versioning for OATS test files. All test files must now include an `oats-file-version` tag.

Full release notes: https://github.com/grafana/oats/releases/tag/v0.6.0

### Add `oats-file-version: "2"` to all test files

All OATS test files must now include the `oats-file-version` field at the top level. The current version is `"2"`.

```yaml
# ✅ Required in all test files
oats-file-version: "2"

docker-compose:
  files:
    - ./docker-compose.yaml
expected:
  metrics:
    - promql: 'uptime_seconds_total{}'
      value: '>= 0'
```

**Why version "2"?** Version "1" represents the old format without an explicit version tag. The version number will only be incremented when migrations are needed.

### File Discovery Changes

**Before:** OATS would discover all `*oats*.yaml` files (except those ending in `-template.yaml`) in the specified directory.

**Now:** OATS only considers files with a `.yaml` or `.yml` extension that contain the `oats-file-version` tag.

### New: `oats-template` Flag

Template files that should be included (via `include:`) but not run as entry points must now use the `oats-template: true` flag:

```yaml
# Template file (e.g., oats-base.yaml)
oats-file-version: "2"
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

This is particularly useful when you have many YAML files in your repository and want to avoid parsing all of them.

### Migration Steps

1. Add `oats-file-version: "2"` to all your test files
2. Add `oats-template: true` to any template files (files that are included but not entry points)
3. (Optional) Consider passing specific file paths instead of directories for better performance

## 0.5.0

⚠️ Breaking Changes - Migration Required Changes to Your YAML Files

This release enforces stricter validation and **removes support for deprecated YAML syntax**. 
You must update your test files when upgrading.

Full release notes: https://github.com/grafana/oats/releases/tag/v0.5.0

### 1. Replace `contains` with `regexp`

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

# ✅ New
traces:
  - traceql: '{}'
    equals: "GET /api"
```

```diff
traces:
  - traceql: '{}'
-   spans:
-     - name: "GET /api"
+   equals: "GET /api"
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


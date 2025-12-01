# Upgrading

The changelog is available in the [releases section](https://github.com/grafana/oats/releases).

This file only contains upgrade notes for breaking changes that require you to modify your existing 
YAML test files.

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


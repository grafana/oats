# Smoke examples

These cases demonstrate individual seed and assertion modes against an
already-running Grafana LGTM stack. Unlike `examples/python`, the remote
fixture does not start or stop the stack.

Install `oats` and `gcx`, then start a local LGTM stack with OTLP/HTTP exposed:

```sh
docker run --rm --name oats-lgtm \
  -p 3000:3000 -p 4317:4317 -p 4318:4318 \
  grafana/otel-lgtm
```

Run a case from this directory with its path, for example:

```sh
oats inline-seed/
```

| Case            | Demonstrates                                          | Additional prerequisite                                         |
| --------------- | ----------------------------------------------------- | --------------------------------------------------------------- |
| `inline-seed/`  | Sending a trace directly with inline OTLP             | LGTM only                                                       |
| `rolldice/`     | Driving an app and checking traces, metrics, and logs | An app on `localhost:8080` exporting to the local OTLP endpoint |
| `custom-check/` | Running a repository-local verification script        | The script's own prerequisites                                  |
| `profile/`      | Querying CPU profiles                                 | An app that produces the expected profiles                      |

Use `oats list` to inspect the complete plan. Tags are also available, for
example `oats --tags traces` or `oats --tags profiles`.

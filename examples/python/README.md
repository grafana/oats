# Python example

This is the self-contained getting-started example: a small instrumented Flask
app, a Compose fixture, and one OATS case covering traces, metrics, and logs.

## Run it

Install `oats` and `gcx`, make sure Docker is running, then run from this
directory:

```sh
oats
```

OATS builds and starts the Flask app, adds a temporary Grafana LGTM stack,
drives `/rolldice`, checks the emitted telemetry, and tears the fixture down.

The files are deliberately kept together so the directory can also be copied
as a starting point for a new OATS project:

- `oats-config.yaml` — lists the case.
- `oats-case.yaml` — declares the input and expected telemetry.
- `docker-compose.oats.yml` — defines the application container.
- `Dockerfile`, `app.py`, and `requirements.txt` — the application.

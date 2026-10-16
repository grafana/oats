# Troubleshoot a failed OATS case with gcx

Use this guide when an OATS assertion fails and you need to determine whether
the fixture, app action, query, assertion, or telemetry path caused it. This
interactive workflow requires OATS v0.11.0 or later, which introduced
`--pause-on-failure`, and a managed Compose fixture. It does not work in CI,
noninteractive sessions, NDJSON output, parallel runs, or k3d/remote fixtures.
See the [OATS releases](https://github.com/grafana/oats/releases) for an
available version. OATS v0.11.0 uses gcx v1.4.0 as its default minimum; preserve
any explicit `--gcx` or `--gcx-version` selection from the failing run.

Follow the gcx-owned [diagnostic guide][diagnostics] for installation, existing
skills, connection setup, evidence limits, and repair approval. This page covers
OATS-specific failure evidence and fixture lifecycle.

## Preserve the failed run's evidence

Before rerunning, record locally:

- The exact CI job or command, commit, OATS version (`oats version`), gcx version
  (`gcx version`), and any explicit `--gcx` / `--gcx-version` options.
- The config and case paths, failed case and assertion, query, expected result,
  and available error/output.
- The fixture type, destination, relevant identifiers, and time window.

Review output before sharing it. Remove credentials and sensitive payloads; keep
full result files, private config paths, service names, and project details out
of public links.

## Reproduce one managed Compose case

Use the same case, source revision, config, and gcx selection as the failure.
Pass the case file (or its narrowest containing directory) so the run does not
schedule unrelated tests. Disable the result cache and enable the interactive
pause:

```sh
oats --config ./oats-config.yaml \
  --no-cache --parallel=1 --format=text --pause-on-failure \
  cases/path/to/failing-case.yaml
```

Keep any relevant flags from the original run, especially `--gcx` or
`--gcx-version`, `--timeout`, `--tags`, and `--lgtm-version`. The pause option
requires an interactive terminal, text output, `--parallel=1`, and managed
Compose fixtures. OATS checks these prerequisites before creating resources.
It pauses after the first failed case in the fixture group; a startup failure or
lost fixture follows the normal error and cleanup path instead.

When a case fails, OATS keeps its managed Compose fixture and private gcx config
alive, prints the failed case and the `--config` / `--context` selection, and
waits for Enter or Ctrl-C. Do not close that OATS process or remove the fixture
while investigating. The temporary config is private: do not share its contents
or credentials. Open another terminal and use gcx with the exact config and
context OATS printed. If the original run selected an explicit gcx binary or
version, use that same selection for the query. Follow the shared guide for
queries and existing gcx skills; give an agent the case, assertion, query,
fixture, time window, identifiers, and available failure evidence.

If the fixture is remote or externally managed, OATS does not tear it down. It
also cannot guarantee the original telemetry is still retained or accessible.
Check with the fixture owner and backend retention policy before querying. The
interactive pause is not supported for remote or k3d fixtures; do not describe a
new local run as reproduction of their original data.

## Diagnose before proposing a repair

Classify the evidence before changing anything:

- **Fixture or startup:** Did the destination become ready, and was the intended
  datasource reachable with the expected access?
- **Application action:** Did the case's input reach the app and perform the
  expected operation?
- **Query or assertion:** Does the query target the right signal, labels, time
  range, and expected value? Healthy telemetry with an incorrect query or
  assertion is not an application failure.
- **Telemetry path:** Is there evidence that the app exported data, a Collector
  received it, and the destination stored it? State unavailable boundaries as
  unobserved; do not infer data loss from missing access.

Suggested prompt:

> This OATS case failed: `<case>`. Here are the original command, assertion,
> query, expected result, fixture details, time window, and failure output. Use
> existing gcx skills to determine whether the fixture, app action, query,
> assertion, or telemetry path explains the failure. Do not weaken the assertion
> to make it pass. Ask before changing anything. If access is missing, say what
> you could not observe rather than inferring data loss.

Propose the smallest repair supported by evidence and get approval before
changing the app, Collector, fixture, query, or assertion. A passing ad hoc gcx
query alone does not prove the OATS case is recovered.

## Clean up and verify the original case

Press Enter (or Ctrl-C) in the paused OATS process to run normal fixture/config
cleanup. The original OATS run remains failed; the pause does not change its
exit result. Force-killing the process can bypass cleanup and leave resources
behind, so check and clean up only resources owned by this run.

After an approved repair, rerun the same case uncached, without the pause flag:

```sh
oats --config ./oats-config.yaml --no-cache --parallel=1 \
  cases/path/to/failing-case.yaml
```

Confirm the original assertion passes and review the fresh evidence. A recreated
run has new timestamps and identifiers; do not present it as the original
failed-run telemetry. Record remaining unknowns and restore any temporary
diagnostic settings.

[diagnostics]: https://github.com/grafana/gcx/blob/v1.4.0/docs/guides/diagnose-missing-telemetry.md

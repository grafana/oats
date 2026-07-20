# ADR-0003: Preserve backend isolation for parallel fixture groups

- **Status:** Accepted
- **Date:** 2026-07-18

## Context

OATS can run derived fixture groups concurrently. A Compose or k3d fixture is
expensive, so sharing one LGTM backend across groups appears attractive. That
would change the isolation boundary, however: unscoped TraceQL, LogQL, or
PromQL could observe another group's telemetry and produce false passes or
failures.

The current design gives each booted local fixture its own backend and uses
unique project names and ephemeral host ports where needed.

## Decision

Keep parallelism hermetic: each parallel-safe group owns its own local backend.
`--parallel N` remains explicit and defaults to one. Compose app-seed groups
are parallel-safe only when OATS manages an ephemeral app port; k3d remains
serial until its port-forward and cluster model are made safe.

Do not add shared-LGTM execution or make the default parallelism automatically
scale with CPU count in the initial integration.

## Consequences

- Parallel runs consume more memory because each local LGTM backend is separate.
- Existing queries do not need hidden service-name scoping changes.
- The behavior is predictable and preserves the current hermeticity guarantee.

## Revisit trigger

Reconsider a shared backend only when a real consumer is resource-bound by
hermetic parallelism and can accept an explicit opt-in mode plus validation of
query isolation. Reconsider automatic parallelism after CI wall-time and
resource measurements provide a safe default for the relevant fixture types.

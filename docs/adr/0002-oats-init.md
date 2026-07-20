# ADR-0002: Defer `oats init`

- **Status:** Deferred
- **Date:** 2026-07-18

## Context

A project generator could create an `oats-config.yaml`, a starter case, mise
pins, and a GitHub Actions workflow. Only the first two are OATS-owned facts;
the application Compose file, tool-version policy, and CI workflow are
consumer-specific choices.

OATS already provides a copyable example and `oats migrate` for existing legacy
projects. Generating a large set of policy files before seeing a real consumer
use the v3 flow risks producing stale or misleading boilerplate.

## Decision

Do not add a full `oats init` command to the initial v3 integration. Revisit it
after at least one downstream consumer has used the new flow and identified
repeated onboarding friction.

If a generator becomes necessary, start with a thin scaffold that creates only
an `oats-config.yaml` and a runnable starter `oats-case.yaml`. mise and CI
workflow files should be explicit opt-in templates, not silent defaults.

## Consequences

- The initial CLI stays smaller and avoids owning consumer project policy.
- The examples and migration command remain the onboarding surface for now.
- A future `init` design should be based on observed downstream repetition,
  rather than guesses about application layout.

## Revisit trigger

Implement a thin scaffold when a downstream consumer has repeated the same
setup steps or when multiple users request project generation with compatible
assumptions.

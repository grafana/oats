# ADR-0001: Container engines for fixture-backed tests

- **Status:** Accepted
- **Date:** 2026-07-18

## Context

OATS currently shells out to Docker, Docker Compose, k3d, and kubectl from its
fixture lifecycle. That makes the fixture package look pluggable while still
coupling it to one container engine. It also forces users who prefer rootless
Podman to change their environment or maintain wrappers.

The case schema should describe the test and its fixture, not the host's
container-engine choice. Compose and k3d also have different capabilities:
k3d is a Kubernetes-in-Docker workflow, while Compose can be implemented by
multiple container engines.

## Decision

OATS will introduce a narrow internal container-engine layer and support Docker
and Podman as first-class engines for Compose fixtures. The layer will:

- centralize command execution, environment handling, logging, cancellation,
  and cleanup;
- expose Compose lifecycle operations such as up, port lookup, exec, logs, and
  teardown through an adapter;
- keep engine selection out of the case schema; and
- preserve Docker as a supported, explicit option.

The host-level selection will be exposed through `--container-runtime` and
`OATS_CONTAINER_RUNTIME`, with `docker`, `podman`, and `auto` as the intended
values. `auto` should prefer Podman when it is available and fall back to
Docker; an explicit engine must not silently fall back to another engine.

The first alternative-engine target is Compose with rootless Podman. k3d and
its Docker image-import path remain a separate adapter until reliable Podman
coverage exists for that workflow. Apple Container is not treated as a
Docker-compatible binary replacement; it would require its own adapter if a
consumer needs it.

## Consequences

- Compose fixtures can be used in environments that do not expose a Docker
  socket.
- The implementation needs an engine compatibility test matrix; swapping the
  executable name is not sufficient because Compose providers and flags can
  differ.
- CI should select its engine explicitly for reproducible runs, while local
  `auto` mode can prefer a rootless Podman installation.
- No case-file migration or schema change is required.

## Implementation

The Docker and rootless-Podman Compose paths are covered by the e2e matrix in
[PR #394](https://github.com/grafana/oats/pull/394).

## Revisit

Add k3d/Podman or Apple Container adapters only with a concrete implementation
and CI coverage.

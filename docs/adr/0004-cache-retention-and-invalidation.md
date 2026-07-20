# ADR-0004: Keep cache retention simple and make invalidation explicit

- **Status:** Accepted
- **Date:** 2026-07-18

## Context

OATS has a skip-when-unchanged cache for green cases. A long-lived cache could
also be bounded by entry count or size, and a consumer could include built
image digests in the key. Both add configuration or integration work, while
we do not currently have measurements from long-lived consumer cache
directories showing that either feature is needed.

The cache currently hashes the case, fixture configuration, gcx version, and
OATS version. It uses TTL-based expiry, lazy removal of expired entries, and an
explicit `oats cache clear` command.

The existing unit tests cover TTL expiry and manual clearing. This ADR is a
scope decision based on the current implementation and lack of observed demand,
not a claim that TTL provides a hard bound on cache size.

## Decision

Use TTL expiry plus manual clearing as the initial cache-retention policy. Do
not add an LRU/size cap or automatic image-digest discovery now.

Document the floating-image-tag caveat. Consumers that rebuild an image under
the same tag must either pin the image by digest, avoid persisting the cache,
or provide a future digest value through the cache key's extension point.

## Consequences

- The cache remains simple and predictable across platforms.
- A pathological long-lived cache may grow until TTL eviction occurs.
- Consumers are responsible for avoiding stale results when fixture contents
  are not represented by the fixture configuration.

## Revisit triggers

Add a size/count cap after observing cache growth in a long-lived environment.
Fold image digests into the key when a real consumer uses floating image tags
with a persisted cache and cannot adopt digest pinning.

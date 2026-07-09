#!/usr/bin/env bash
set -euo pipefail
# The harness copies files/ into a fresh t.TempDir() per run, so this proof
# directory starts empty every run — the markers cannot be stale.
dir="$(cd "$(dirname "$0")" && pwd)"
proof="$dir/.parallel-proof"
mkdir -p "$proof"
touch "$proof/alpha"
for _ in $(seq 1 50); do
	if [ -f "$proof/beta" ]; then
		exit 0
	fi
	sleep 0.2
done
echo "beta verifier never overlapped with alpha" >&2
exit 1

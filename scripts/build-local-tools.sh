#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
bin_dir="${1:-$root/bin}"
mkdir -p "$bin_dir"
gcx_version="$(bash "$root/scripts/gcx-version.sh")"

mise run build -- "$bin_dir/oats"

# CI and mise-based development already have the pinned gcx binary installed.
# Reuse it instead of downloading and rebuilding a second copy. Direct
# `go test ./tests/e2e` remains self-contained by falling back to the version
# pinned in mise.toml when mise is unavailable.
gcx_bin=""
if command -v mise >/dev/null 2>&1; then
	gcx_bin=$(mise -C "$root" which gcx 2>/dev/null || true)
fi
if [[ -n "$gcx_bin" && -x "$gcx_bin" ]]; then
	install -m 0755 "$gcx_bin" "$bin_dir/gcx"
else
	# Keep this pin in mise.toml so Renovate updates the tool version rather than
	# this script.
	GOBIN="$bin_dir" GOWORK=off go install "github.com/grafana/gcx/cmd/gcx@v${gcx_version}"
fi

printf 'built %s/oats and %s/gcx\n' "$bin_dir" "$bin_dir"

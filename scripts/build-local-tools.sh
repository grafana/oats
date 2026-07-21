#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
bin_dir="${1:-$root/bin}"
mkdir -p "$bin_dir"

mise -C "$root" run build -- "$bin_dir/oats"

# CI and mise-based development already have the pinned gcx binary installed.
# Reuse it instead of downloading and rebuilding a second copy.
gcx_bin=""
gcx_bin=$(mise -C "$root" which gcx 2>/dev/null || true)
if [[ -n "$gcx_bin" && -x "$gcx_bin" ]]; then
	install -m 0755 "$gcx_bin" "$bin_dir/gcx"
else
	echo "mise-managed gcx is required; run mise install" >&2
	exit 1
fi

printf 'built %s/oats and %s/gcx\n' "$bin_dir" "$bin_dir"

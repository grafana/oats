#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
bin_dir="${1:-$root/bin}"
mkdir -p "$bin_dir"

# Local smoke/e2e runs need both binaries. Consumer repos should still only
# install `oats`; OATS owns the gcx bootstrap for fixture-backed runs. Keep the
# version in mise.toml so Renovate updates the tool pin rather than this script.
gcx_version=$(awk -F'"' '$2 == "aqua:grafana/gcx" { print $4; exit }' "$root/mise.toml")
if [[ -z "$gcx_version" ]]; then
	echo "aqua:grafana/gcx is missing from $root/mise.toml" >&2
	exit 1
fi
gcx_version="${gcx_version#v}"

GOWORK=off go -C "$root" build -buildvcs=false -o "$bin_dir/oats" .
GOBIN="$bin_dir" GOWORK=off go install "github.com/grafana/gcx/cmd/gcx@v${gcx_version}"

printf 'built %s/oats and %s/gcx\n' "$bin_dir" "$bin_dir"

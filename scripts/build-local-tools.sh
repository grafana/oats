#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
bin_dir="${1:-$root/bin}"
mkdir -p "$bin_dir"

# Keep the gcx bootstrap logic owned by OATS so consumer repos only need to
# fetch/build OATS itself.
: "${GCX_VERSION:=v0.4.0}"

GOWORK=off go -C "$root" build -buildvcs=false -o "$bin_dir/oats" ./cmd/v2
GOBIN="$bin_dir" GOWORK=off go install "github.com/grafana/gcx/cmd/gcx@${GCX_VERSION}"

printf 'built %s/oats and %s/gcx\n' "$bin_dir" "$bin_dir"

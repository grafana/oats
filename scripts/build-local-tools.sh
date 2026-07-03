#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
bin_dir="${1:-$root/bin}"
mkdir -p "$bin_dir"

# Local smoke/e2e runs need both binaries. Consumer repos should still only
# install `oats`; OATS owns the gcx bootstrap for fixture-backed runs.
: "${GCX_VERSION:=v0.4.0}"

GOWORK=off go -C "$root" build -buildvcs=false -o "$bin_dir/oats" .
GOBIN="$bin_dir" GOWORK=off go install "github.com/grafana/gcx/cmd/gcx@${GCX_VERSION}"

printf 'built %s/oats and %s/gcx\n' "$bin_dir" "$bin_dir"

#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
output="${1:-$root/oats}"
gcx_version="$(bash "$root/scripts/gcx-version.sh")"

mkdir -p "$(dirname "$output")"
GOWORK=off go -C "$root" build \
	-buildvcs=false \
	-ldflags "-X github.com/grafana/oats/internal/cli.DefaultGCXVersion=$gcx_version" \
	-o "$output" .

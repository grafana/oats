#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
version="$(awk -F'"' '$2 == "aqua:grafana/gcx" { print $4; exit }' "$root/mise.toml")"
if [[ -z "$version" ]]; then
	echo "aqua:grafana/gcx is missing from $root/mise.toml" >&2
	exit 1
fi

printf '%s\n' "${version#v}"

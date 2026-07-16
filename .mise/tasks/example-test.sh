#!/usr/bin/env bash
#MISE description="Run the self-contained examples"

set -euo pipefail

root=$(git rev-parse --show-toplevel)
oats_bin="${OATS_BIN:-$root/oats}"
gcx_bin="${GCX_BIN:-gcx}"

if [[ ! -x "$oats_bin" ]]; then
	go build -o "$oats_bin" "$root/."
fi

if ! command -v "$gcx_bin" >/dev/null 2>&1; then
	echo "gcx is required; install it or set GCX_BIN" >&2
	exit 1
fi

list_example() {
	local dir=$1
	echo "==> listing $dir"
	(cd "$root/$dir" && "$oats_bin" list)
}

run_example() {
	local dir=$1
	shift
	echo "==> running $dir $*"
	(
		cd "$root/$dir"
		"$oats_bin" --gcx "$gcx_bin" --no-cache --timeout=90s --interval=2s --parallel=1 "$@"
	)
}

# Always validate discovery for every example, including smoke cases whose
# remote app/profile prerequisites are intentionally supplied by the user.
list_example examples/python
list_example examples/smoke
list_example examples/fixtures

# These examples own all of their runtime dependencies. The fixture example is
# split so Compose and k3d can still be run independently when debugging.
run_example examples/python
run_example examples/fixtures --tags compose

if ! command -v docker >/dev/null 2>&1 || ! command -v k3d >/dev/null 2>&1 || ! command -v kubectl >/dev/null 2>&1; then
	echo "docker, k3d, and kubectl are required for the k3d example" >&2
	exit 1
fi

# k3d imports the LGTM image into the cluster, so ensure it exists in the local
# Docker image store before OATS starts the fixture.
docker pull docker.io/grafana/otel-lgtm:latest
run_example examples/fixtures --tags k3d

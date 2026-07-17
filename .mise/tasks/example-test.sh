#!/usr/bin/env bash
#MISE description="Run the self-contained examples"

set -euo pipefail

root=$(git rev-parse --show-toplevel)
oats_bin="${OATS_BIN:-$root/oats}"
gcx_bin="${GCX_BIN:-gcx}"
mode="${OATS_EXAMPLE_MODE:-all}"

case "$mode" in
all | apps | compose)
	;;
*)
	echo "unknown OATS_EXAMPLE_MODE: $mode (want all, apps, or compose)" >&2
	exit 1
	;;
esac

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

run_python_and_k3d_in_parallel() {
	local status=0
	local -a pids=()

	run_example examples/python &
	pids+=("$!")
	run_example examples/fixtures --tags k3d &
	pids+=("$!")

	for pid in "${pids[@]}"; do
		if ! wait "$pid"; then
			status=1
		fi
	done
	return "$status"
}

# Always validate discovery for every example, including smoke cases whose
# remote app/profile prerequisites are intentionally supplied by the user.
if [[ "$mode" != compose ]]; then
	list_example examples/python
	list_example examples/smoke
fi
list_example examples/fixtures

# These examples own all of their runtime dependencies. The Python Compose
# example and the k3d example can run together; only one Compose stack runs at
# a time because fixture startup prunes Docker networks.
if ! command -v docker >/dev/null 2>&1; then
	echo "docker is required for the runtime examples" >&2
	exit 1
fi

docker pull docker.io/grafana/otel-lgtm:latest
if [[ "$mode" == compose ]]; then
	run_example examples/fixtures --tags compose
else
	if ! command -v k3d >/dev/null 2>&1 || ! command -v kubectl >/dev/null 2>&1; then
		echo "k3d and kubectl are required for the app examples" >&2
		exit 1
	fi
	run_python_and_k3d_in_parallel
fi

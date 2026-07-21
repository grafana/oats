#!/usr/bin/env bash
set -euo pipefail

readonly minimum_coverage="${OATS_MIN_COVERAGE:-80.0}"
readonly profile="${COVERAGE_PROFILE:-$(mktemp)}"

cleanup() {
	if [[ -z "${COVERAGE_PROFILE:-}" ]]; then
		rm -f "$profile"
	fi
}
trap cleanup EXIT

mapfile -t packages < <(
	GOFLAGS=-buildvcs=false go list ./... | grep -v '^github.com/grafana/oats/tests/e2e$'
)

GOFLAGS=-buildvcs=false go test -coverprofile="$profile" "${packages[@]}"

coverage="$(go tool cover -func="$profile" | awk '$1 == "total:" {gsub("%", "", $3); print $3}')"
if [[ -z "$coverage" ]]; then
	echo "could not determine total test coverage" >&2
	exit 1
fi

printf 'total coverage: %s%% (minimum: %s%%)\n' "$coverage" "$minimum_coverage"
awk -v coverage="$coverage" -v minimum="$minimum_coverage" \
	'BEGIN { if ((coverage + 0) < (minimum + 0)) { exit 1 } }'

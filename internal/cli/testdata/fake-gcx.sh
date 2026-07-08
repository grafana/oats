#!/usr/bin/env bash
# fake-gcx: a tiny stand-in for the gcx CLI used by oats integration tests.
#
# It accepts a "--context X" prefix (stripped and ignored), then dispatches on
# the verb chain. Output is deterministic and matches what the integration
# tests assert on.

set -euo pipefail

# Drop leading global flags so the rest of the args mirror what a case yaml
# would produce via signalcmd.
while [[ $# -gt 0 ]]; do
	case "${1:-}" in
	--context | --config)
		shift 2
		;;
	*)
		break
		;;
	esac
done

json=false
for arg in "$@"; do
	if [[ "$arg" == "-o" || "$arg" == "--output" ]]; then
		json=true
	fi
done

# Last positional arg (portable: bash 3.2 lacks ${*: -1} negative indexing).
query=""
for query in "$@"; do :; done
is_missing=false
if [[ "$query" == *"missing"* ]]; then
	is_missing=true
fi

case "${1:-}.${2:-}" in
traces.search)
	if [[ "$is_missing" == true ]]; then
		if [[ "$json" == true ]]; then
			cat <<'EOF'
{"status":"success","data":{"result":[]}}
EOF
		fi
		exit 0
	fi
	if [[ "$json" == true ]]; then
		cat <<'EOF'
{"status":"success","data":{"result":[{"name":"seed-operation","attributes":{"service.name":"gcx-e2e-seed","trace_id":"abc123def456"}}]}}
EOF
	else
		cat <<'EOF'
Trace IDs                          Service          Span
abc123def456                       gcx-e2e-seed     seed-operation
EOF
	fi
	;;
logs.query)
	if [[ "$is_missing" == true ]]; then
		if [[ "$json" == true ]]; then
			cat <<'EOF'
{"status":"success","data":{"resultType":"streams","result":[]}}
EOF
		fi
		exit 0
	fi
	if [[ "$json" == true ]]; then
		cat <<'EOF'
{"status":"success","data":{"resultType":"streams","result":[{"stream":{"service_name":"gcx-e2e-seed","trace_id":"abc123def456"},"values":[["1700000000000000000","seed-log-line"]]}]}}
EOF
	else
		cat <<'EOF'
time                  service       body
2026-06-12T09:00:00Z  gcx-e2e-seed  seed-log-line
EOF
	fi
	;;
metrics.query)
	if [[ "$is_missing" == true ]]; then
		cat <<'EOF'
{"status":"success","data":{"resultType":"vector","result":[]}}
EOF
		exit 0
	fi
	# Static JSON shaped like Prometheus instant query output.
	cat <<'EOF'
{"status":"success","data":{"resultType":"vector","result":[{"metric":{"__name__":"seed_counter_total","service_name":"gcx-e2e-seed"},"value":[1700000000,"42"]}]}}
EOF
	;;
profiles.query)
	if [[ "$is_missing" == true ]]; then
		cat <<'EOF'
{"flamebearer":{"names":[]}}
EOF
		exit 0
	fi
	cat <<'EOF'
{"flamebearer":{"names":["main","worker"]}}
EOF
	;;
*)
	echo "fake-gcx: unsupported verb chain: $*" >&2
	exit 2
	;;
esac

#!/usr/bin/env bash
# fake-gcx: a tiny stand-in for the gcx CLI used by oats integration tests.
#
# It accepts a "--context X" prefix (stripped and ignored), then dispatches on
# the verb chain. Output is deterministic and matches what the integration
# tests assert on.

set -euo pipefail

# Drop a leading "--context X" pair so the rest of the args mirror what
# a case yaml would produce via signalcmd.
if [[ "${1:-}" == "--context" ]]; then
	shift 2
fi

json=false
for arg in "$@"; do
	if [[ "$arg" == "-o" || "$arg" == "--output" ]]; then
		json=true
	fi
done

case "${1:-}.${2:-}" in
traces.search)
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
	# Static JSON shaped like Prometheus instant query output.
	cat <<'EOF'
{"status":"success","data":{"resultType":"vector","result":[{"metric":{"__name__":"seed_counter_total","service_name":"gcx-e2e-seed"},"value":[1700000000,"42"]}]}}
EOF
	;;
profiles.query)
	cat <<'EOF'
{"flamebearer":{"names":["main","worker"]}}
EOF
	;;
*)
	echo "fake-gcx: unsupported verb chain: $*" >&2
	exit 2
	;;
esac

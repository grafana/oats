#!/usr/bin/env bash
set -euo pipefail
grafana_url="$(awk '/server:/ {print $2; exit}' "${GCX_CONFIG:?}")"
curl -fsS "${grafana_url}/api/health" >/dev/null
echo "custom check ok"

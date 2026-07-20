#!/usr/bin/env bash
set -euo pipefail
curl -fsS "${OATS_GRAFANA_URL:?}/api/health" >/dev/null
echo "custom check ok"

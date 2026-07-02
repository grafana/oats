#!/usr/bin/env bash
set -euo pipefail
python3 - <<'PY'
import os, urllib.request
urllib.request.urlopen(os.environ['OATS_GRAFANA_URL'] + '/api/health').read()
PY
echo "custom check ok"

#!/bin/sh
set -eu

curl --fail --silent --show-error "${OATS_GRAFANA_URL:?}/api/health" >/dev/null

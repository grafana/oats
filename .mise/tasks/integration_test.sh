#!/usr/bin/env bash
#MISE description="Run Integration tests"

set -euo pipefail

mise run build
./oats -timeout 5m tests/e2e

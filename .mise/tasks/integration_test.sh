#!/usr/bin/env bash
#MISE description="Run Integration tests"

set -euo pipefail

mise run build
./oats tests/e2e

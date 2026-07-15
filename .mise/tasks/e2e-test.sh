#!/usr/bin/env bash
#MISE description="Run end-to-end tests"

set -euo pipefail

# The dedicated v3 e2e harness lands later in the stack. On lower stacked PRs,
# keep this workflow green without trying to run the removed schema-v2 case via
# the new CLI.
if [[ ! -f tests/e2e/e2e_test.go ]]; then
	echo "No v3 e2e suite on this branch yet; skipping."
	exit 0
fi

mise run build
./oats run --timeout=5m --config tests/e2e/oats-config.yaml

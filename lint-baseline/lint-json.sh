#!/usr/bin/env bash
set -euo pipefail
exec bash "${BASH_SOURCE[0]%/*}/with-lint-env.sh" golangci-lint run \
  --config "$1" --enable-only "$BASELINE_LINTERS" --allow-parallel-runners --uniq-by-line=false \
  --show-stats=false --max-same-issues=0 --max-issues-per-linter=0 --issues-exit-code=0 \
  --output.text.path= --output.json.path="$2" ./...

#!/usr/bin/env bash
set -euo pipefail
export GOFLAGS=-mod=readonly GOTOOLCHAIN=local GOWORK=off
token=$(openssl rand -hex 16)
printf '::stop-commands::%s\n' "$token"
trap 'printf "\n::%s::\n" "$token"' EXIT
golangci-lint run --config "$1" --enable-only "$BASELINE_LINTERS" --allow-parallel-runners --uniq-by-line=false \
  --show-stats=false --max-same-issues=0 --max-issues-per-linter=0 --issues-exit-code=0 \
  --output.text.path= --output.json.path="$2" ./...

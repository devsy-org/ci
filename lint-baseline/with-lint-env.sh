#!/usr/bin/env bash
set -euo pipefail
export GOFLAGS=-mod=readonly GOTOOLCHAIN=local GOWORK=off
token=$(openssl rand -hex 16)
printf '::stop-commands::%s\n' "$token"
trap 'printf "\n::%s::\n" "$token"' EXIT
"$@"

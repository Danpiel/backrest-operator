#!/usr/bin/env bash
# Go unit tests (mirrors ci.yaml job go).
set -euo pipefail

ROOT="$(cd .. && pwd)"
export GOMODCACHE="${ROOT}/cache/gomod"
export GOCACHE="${ROOT}/cache/gocache"
export GOPATH="${ROOT}/cache/gobin"
export PATH="${GOPATH}/bin:${PATH}"
mkdir -p "${GOMODCACHE}" "${GOCACHE}" "${GOPATH}"

git config --global --add safe.directory "$(pwd)"

CI_CONFIG_DIR="${CI_CONFIG_DIR:-../ci-config}"
# shellcheck source=ensure-go.sh
source "${CI_CONFIG_DIR}/concourse/scripts/ensure-go.sh"

go test ./...
go vet ./...
go build -o /dev/null ./cmd/operator
go build -o /dev/null ./cmd/mcp

echo "unit-test passed"

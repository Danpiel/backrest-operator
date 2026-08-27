#!/usr/bin/env bash
# Helm lint (mirrors ci.yaml job helm-lint).
set -euo pipefail

git config --global --add safe.directory "$(pwd)"

if ! command -v helm >/dev/null 2>&1; then
  curl -fsSL https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | bash
fi

helm lint charts/backrest-operator
helm template test charts/backrest-operator >/dev/null

echo "helm-lint passed"

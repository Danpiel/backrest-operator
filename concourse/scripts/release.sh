#!/usr/bin/env bash
# Tag release: test, images, Helm OCI chart, GitHub Release.
# Scripts from pipeline-src; git tree is cwd (tag checkout, dir: app-git).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

bash "${SCRIPT_DIR}/unit-test.sh"
bash "${SCRIPT_DIR}/helm-lint.sh"

# shellcheck source=image-metadata.sh
source "${SCRIPT_DIR}/image-metadata.sh"

if [ "${IS_RELEASE}" != "true" ]; then
  echo "ERROR: release job requires a v* tag checkout" >&2
  exit 1
fi

bash "${SCRIPT_DIR}/build-images.sh"

# shellcheck source=ensure-gh.sh
source "${SCRIPT_DIR}/ensure-gh.sh"

if ! command -v helm >/dev/null 2>&1; then
  curl -fsSL https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | bash
fi

mkdir -p dist
helm package charts/backrest-operator \
  --version "${CHART_VERSION}" \
  --app-version "${CHART_VERSION}" \
  -d dist

CHART="$(ls dist/backrest-operator-*.tgz | head -n1)"

helm registry login ghcr.io -u "${GHCR_USERNAME}" -p "${GHCR_PASSWORD}"
helm push "${CHART}" "oci://ghcr.io/${OWNER}/charts"

export GH_TOKEN="${GITHUB_TOKEN}"
GITHUB_REPOSITORY="${GITHUB_REPOSITORY:-Reactive-Network/backrest-operator}"
VERSION="${REF#v}"
gh release create "v${VERSION}" \
  --repo "${GITHUB_REPOSITORY}" \
  --title "Release v${VERSION}" \
  --generate-notes \
  "${CHART}"

echo "Release v${VERSION} published"

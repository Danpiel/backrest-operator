#!/usr/bin/env bash
# Tag release: test, images, Helm OCI chart, GitHub Release.
set -euo pipefail

CI_CONFIG_DIR="${CI_CONFIG_DIR:-../ci-config}"
export CI_CONFIG_DIR

bash concourse/scripts/unit-test.sh
bash concourse/scripts/helm-lint.sh

# shellcheck source=image-metadata.sh
source "$(dirname "$0")/image-metadata.sh"

if [ "${IS_RELEASE}" != "true" ]; then
  echo "ERROR: release job requires a v* tag checkout" >&2
  exit 1
fi

bash concourse/scripts/build-images.sh

# shellcheck source=ensure-gh.sh
source "${CI_CONFIG_DIR}/concourse/scripts/ensure-gh.sh"

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
VERSION="${REF#v}"
gh release create "v${VERSION}" \
  --repo "Danpiel/backrest-operator" \
  --title "Release v${VERSION}" \
  --generate-notes \
  "${CHART}"

echo "Release v${VERSION} published"

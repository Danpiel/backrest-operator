#!/usr/bin/env bash
# Build and push operator + MCP images via remote BuildKit.
set -euo pipefail

CI_SHARED_DIR="${CI_SHARED_DIR:-../ci-shared}"
if [ ! -f "${CI_SHARED_DIR}/concourse/scripts/buildkit-remote.sh" ]; then
  echo "CI_SHARED_DIR must point at Reactive-Network/ci checkout" >&2
  exit 1
fi

# shellcheck source=image-metadata.sh
source "$(dirname "$0")/image-metadata.sh"
# shellcheck source=buildkit-remote.sh
source "${CI_SHARED_DIR}/concourse/scripts/buildkit-remote.sh"

buildkit_build_dockerfile() {
  local cache_repo="$1"
  local dockerfile="$2"
  shift 2
  local -a images=("$@")

  ensure_buildctl

  local -a cache_args=()
  local cache_ref="${BUILDKIT_CACHE_REGISTRY}/cache/${cache_repo}:buildcache"
  cache_args+=(--import-cache "type=registry,ref=${cache_ref}")
  cache_args+=(--export-cache "type=registry,ref=${cache_ref},mode=max,ignore-error=true")

  local -a output_args=()
  for image in "${images[@]}"; do
    output_args+=(--output "$(image_output_arg "$image")")
  done

  echo "BuildKit ${dockerfile}: ${images[*]}"
  buildctl --addr "${BUILDKIT_HOST}" build \
    --frontend dockerfile.v0 \
    --local context=. \
    --local dockerfile=. \
    --opt "filename=${dockerfile}" \
    --opt platform=linux/amd64 \
    "${cache_args[@]}" \
    "${output_args[@]}"
}

REGISTRY="${REGISTRY:-ghcr.io}"
setup_registry_auth "${REGISTRY}" "${GHCR_USERNAME}" "${GHCR_PASSWORD}"
verify_ghcr_push_access "${OWNER}/backrest-operator"
verify_ghcr_push_access "${OWNER}/backrest-mcp"

buildkit_build_dockerfile "${OWNER}/backrest-operator" "Dockerfile" "${OPERATOR_TAGS[@]}"
buildkit_build_dockerfile "${OWNER}/backrest-mcp" "Dockerfile.mcp" "${MCP_TAGS[@]}"

echo "Images pushed"

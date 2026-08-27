#!/usr/bin/env bash
# release-manual step (d): run release promote + GitHub Release with bumped tag.
set -euo pipefail

if [ -z "${RELEASE_VERSION:-}" ]; then
  if [ ! -f ../release-meta/RELEASE_VERSION ]; then
    echo "ERROR: RELEASE_VERSION unset and ../release-meta/RELEASE_VERSION missing" >&2
    exit 1
  fi
  RELEASE_VERSION="$(cat ../release-meta/RELEASE_VERSION)"
  export RELEASE_VERSION
fi

exec bash "$(dirname "$0")/release.sh"

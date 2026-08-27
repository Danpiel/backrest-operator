#!/usr/bin/env bash
# Install GitHub CLI when missing (fallback for builder-image tags before builder-1d296957).
set -euo pipefail

if command -v gh >/dev/null 2>&1; then
  return 0 2>/dev/null || exit 0
fi

GH_VERSION="${GH_VERSION:-2.63.2}"

arch="$(uname -m)"
case "$arch" in
  x86_64) goarch=amd64 ;;
  aarch64|arm64) goarch=arm64 ;;
  *)
    echo "ERROR: unsupported architecture for gh: ${arch}" >&2
    exit 1
    ;;
esac

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

tarball="gh_${GH_VERSION}_linux_${goarch}.tar.gz"
curl -fsSL "https://github.com/cli/cli/releases/download/v${GH_VERSION}/${tarball}" \
  | tar xz -C "$tmpdir"
install -m 755 "${tmpdir}/gh_${GH_VERSION}_linux_${goarch}/bin/gh" /usr/local/bin/gh
hash -r

echo "Installed gh $(gh --version | head -1)"

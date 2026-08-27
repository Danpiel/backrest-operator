#!/usr/bin/env bash
# Install Go from repo go.mod when missing (builder-image has Docker, not Go).
set -euo pipefail

if command -v go >/dev/null 2>&1; then
  :
else
mod="${GO_MOD:-go.mod}"
if [ ! -f "$mod" ]; then
  echo "ERROR: ${mod} not found; cannot determine Go version" >&2
  exit 1
fi

GO_VERSION="$(awk '/^go / { print $2; exit }' "$mod")"
if [ -z "$GO_VERSION" ]; then
  echo "ERROR: no 'go' directive in ${mod}" >&2
  exit 1
fi

arch="$(uname -m)"
case "$arch" in
  x86_64) goarch=amd64 ;;
  aarch64|arm64) goarch=arm64 ;;
  *)
    echo "ERROR: unsupported architecture: ${arch}" >&2
    exit 1
    ;;
esac

GO_ROOT="${GO_ROOT:-/usr/local/go}"
GO_TAR="go${GO_VERSION}.linux-${goarch}.tar.gz"
GO_URL="https://go.dev/dl/${GO_TAR}"

echo "Installing Go ${GO_VERSION} (${goarch}) from ${GO_URL}"
tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT
curl -fsSL "$GO_URL" -o "${tmpdir}/${GO_TAR}"
rm -rf "$GO_ROOT"
tar -C /usr/local -xzf "${tmpdir}/${GO_TAR}"
export PATH="${GO_ROOT}/bin:${PATH}"
hash -r

go version
fi

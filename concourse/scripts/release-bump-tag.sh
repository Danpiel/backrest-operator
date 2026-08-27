#!/usr/bin/env bash
# Compute next stable semver tag, create + push on current HEAD (R9-b step b+c).
set -euo pipefail

git config --global --add safe.directory "$(pwd)"

git fetch --tags origin 2>/dev/null || true

latest_stable="$(
  git tag -l 'v[0-9]*.[0-9]*.[0-9]*' \
    | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' \
    | sort -V \
    | tail -1 \
    || true
)"

if [ -z "${latest_stable}" ]; then
  next_tag="v0.1.0"
else
  ver="${latest_stable#v}"
  major="${ver%%.*}"
  rest="${ver#*.}"
  minor="${rest%%.*}"
  next_minor=$((minor + 1))
  next_tag="v${major}.${next_minor}.0"
fi

if git rev-parse "refs/tags/${next_tag}" >/dev/null 2>&1; then
  echo "ERROR: tag ${next_tag} already exists" >&2
  exit 1
fi

commit_sha="$(git rev-parse HEAD)"
bash concourse/scripts/ensure-gh.sh
export GH_TOKEN="${GITHUB_TOKEN}"
github_repo="${GITHUB_REPOSITORY:-Reactive-Network/backrest-operator}"

gh api \
  -X POST "repos/${github_repo}/git/refs" \
  -f "ref=refs/tags/${next_tag}" \
  -f "sha=${commit_sha}"

out_dir="../release-meta"
mkdir -p "${out_dir}"
printf '%s\n' "${next_tag}" > "${out_dir}/RELEASE_VERSION"

echo "Created and pushed tag ${next_tag} at ${commit_sha}"

#!/usr/bin/env bash
set -euo pipefail

: "${GITHUB_REPOSITORY:?}" "${GITHUB_REF_NAME:?}" "${GITHUB_SHA:?}" "${GH_TOKEN:?}"
test "$GITHUB_REPOSITORY" = bruin-data/ingestr
python3 hack/release_manifest.py check --tag "$GITHUB_REF_NAME" --commit "$GITHUB_SHA"
test -s release-assets/ingestr-manifest.v1.sigstore.json

cosign verify-blob --bundle release-assets/ingestr-manifest.v1.sigstore.json \
  --trusted-root "$HOME/.sigstore/root/tuf-repo-cdn.sigstore.dev/targets/trusted_root.json" \
  --certificate-identity "https://github.com/bruin-data/ingestr/.github/workflows/release.yml@refs/tags/$GITHUB_REF_NAME" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-github-workflow-repository bruin-data/ingestr \
  --certificate-github-workflow-ref "refs/tags/$GITHUB_REF_NAME" \
  --certificate-github-workflow-sha "$GITHUB_SHA" release-assets/ingestr-manifest.v1.json

# Never adopt or overwrite an existing release, even a draft from a failed run.
temp=$(mktemp -d)
trap 'rm -rf "$temp"' EXIT
gh api --paginate "repos/$GITHUB_REPOSITORY/releases?per_page=100" --jq '.[].tag_name' > "$temp/tags"
if grep -Fxq "$GITHUB_REF_NAME" "$temp/tags"; then
  echo 'Release already exists; refusing to overwrite.' >&2
  exit 1
fi
test "$(git rev-parse HEAD)" = "$GITHUB_SHA"
test "$(git ls-remote origin "refs/tags/$GITHUB_REF_NAME" "refs/tags/$GITHUB_REF_NAME^{}" | tail -1 | cut -f1)" = "$GITHUB_SHA"

gh release create "$GITHUB_REF_NAME" --repo "$GITHUB_REPOSITORY" \
  --verify-tag --target "$GITHUB_SHA" --draft --latest=false --title "$GITHUB_REF_NAME" --generate-notes
gh release upload "$GITHUB_REF_NAME" release-assets/* --repo "$GITHUB_REPOSITORY"
gh release view "$GITHUB_REF_NAME" --repo "$GITHUB_REPOSITORY" --json isDraft,assets > "$temp/release.json"
python3 - "$temp/release.json" "$GITHUB_REF_NAME" <<'PY'
import json
from pathlib import Path
import sys
from hack.release_manifest import BUNDLE, MANIFEST, PLATFORMS

release = json.loads(Path(sys.argv[1]).read_text())
local = {p.name for p in Path("release-assets").iterdir()}
expected = set(PLATFORMS.values()) | {MANIFEST, BUNDLE, f"ingestr_{sys.argv[2][1:]}_checksums.txt"}
if not release["isDraft"]:
    raise SystemExit("release was published prematurely")
names = [a["name"] for a in release["assets"]]
if len(names) != len(expected) or set(names) != expected or local != expected:
    raise SystemExit("incomplete or unexpected assets")
PY
gh release download "$GITHUB_REF_NAME" --repo "$GITHUB_REPOSITORY" --dir "$temp/download"
diff -r release-assets "$temp/download"
# This is the only publication/latest transition. Failure above leaves a draft.
gh release edit "$GITHUB_REF_NAME" --repo "$GITHUB_REPOSITORY" --draft=false --latest

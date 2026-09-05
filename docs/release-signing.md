# Authenticated ingestr releases

The producer contract begins with releases made by the updated
`.github/workflows/release.yml`. Older releases are **not** retroactively
authenticated. This change does not yet add verification to `install.sh`, the
public installer endpoint, the Python wrapper, or Bruin.

## Release contract v1

Only canonical stable `vMAJOR.MINOR.PATCH` tags are accepted (no leading zeros,
prerelease identifiers, or build metadata). The release contains eight assets:

* `ingestr_Darwin_x86_64.tar.gz`, `ingestr_Darwin_arm64.tar.gz`
* `ingestr_Linux_x86_64.tar.gz`, `ingestr_Linux_arm64.tar.gz`
* `ingestr_Windows_x86_64.zip`
* `ingestr_<version>_checksums.txt` (existing format, now including Windows)
* `ingestr-manifest.v1.json`
* `ingestr-manifest.v1.sigstore.json` (Sigstore bundle)

The manifest has `schema_version: 1`, `repository: "bruin-data/ingestr"`, `tag`
(including `v`), `version` (without `v`), `source_commit` (40 lowercase hex
characters), and `platforms` (exactly the five GOOS/GOARCH pairs above). Each
entry has `goos`, `goarch`, `archive: {name, format, size, sha256}` and
`executable: {path, size, sha256}`. Sizes are positive byte counts; hashes are
lowercase SHA-256 hex. Format is `tar.gz` or `zip`. Executable member paths are
exactly `ingestr` or `ingestr.exe` at archive root. Unknown/duplicate fields,
duplicate/missing platforms, unexpected names, and inconsistent identities
must be rejected. `hack/release_manifest.py` is the producer's executable
contract, not a replacement for a consumer's authenticated parsing policy.

GoReleaser split jobs package all platforms without publishing. Run-scoped
GitHub artifacts, additionally separated by run attempt, replace the old
short-SHA build caches. Rerun **all jobs**, not only failed jobs, so no build
from a previous attempt can be reused. Each final packaged executable is run
on its native architecture and must print exactly `ingestr version <tag>`.
The signing job hashes the actual same-run archives and their executable
members, serializes deterministic UTF-8 JSON, and signs those **exact bytes**.
Do not reserialize a downloaded manifest before signature verification.

Cosign uses GitHub OIDC, ephemeral keys, Fulcio certificates, and Rekor signing
evidence. The installer action is pinned to a reviewed commit, which embeds
the SHA-256 of its Cosign v3.0.6 bootstrap. Thus the bootstrap is not trusted
merely because a checksum was downloaded beside it. Maintain this action pin
and its embedded tool version together through reviewed updates.

Only the publishing job gains `contents: write` and signing `id-token: write`;
build jobs have read-only repository access. Existing Docker permissions are
separate. Publication also waits for both native Docker image tests. After
local verification, the publisher creates a draft, uploads
all eight assets without overwrite, checks the exact remote set, downloads
and byte-compares every asset, then publishes and marks latest in one edit.
Failures leave an unpublished draft. Existing releases, including drafts,
are refused, not adopted, replaced, or automatically deleted. Installer/PyPI
checks run after publication; they no longer control a second promotion.
The public installer needs publicly downloadable release assets and cannot
consume a private draft. Its end-to-end checks therefore remain post-publication;
the pre-publication gate runs the actual packaged executables on all five
native platforms instead. Adding authenticated public-installer verification
is separate follow-up work, not an implicit gate in this producer contract.

## Future installer verification policy

Bootstrap the verifier and Sigstore trust material from the **trusted
installer distribution**, never from the unverified release being installed.
For online trust updates, use Sigstore's TUF-authenticated root update flow
(`cosign initialize` with its embedded bootstrap root). For offline use,
retain the resulting authenticated `trusted_root.json` with the verifier;
its path commonly is
`~/.sigstore/root/tuf-repo-cdn.sigstore.dev/targets/trusted_root.json`.
An arbitrary downloaded root is not a trust anchor. Plan reviewed root/tool
updates and retain historical verification keys needed for older releases.

Given a user-requested exact tag, verify with Cosign v3.0.6 or a reviewed
compatible successor:

```bash
TAG=v1.2.3 # requested version, not a tag taken on faith from the manifest
cosign verify-blob ingestr-manifest.v1.json \
  --bundle ingestr-manifest.v1.sigstore.json \
  --trusted-root /trusted/path/trusted_root.json \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity "https://github.com/bruin-data/ingestr/.github/workflows/release.yml@refs/tags/$TAG" \
  --certificate-github-workflow-repository bruin-data/ingestr \
  --certificate-github-workflow-ref "refs/tags/$TAG"
```

Bundle plus explicit trusted root supports offline evidence verification;
`--offline` alone is not a substitute for a local trusted root. Never use
`--insecure-ignore-tlog`, `--insecure-ignore-sct`, or skip identity checks.
Require verified transparency evidence and certificate validity **at the
verified signing time**, not just certificate validity at today's wall clock.
Do not accept an unsigned timestamp supplied by the manifest. A short-lived
certificate expiring after signing must not invalidate a correctly evidenced
historical release. Offline verification does not provide fresh revocation
knowledge.

After signature verification, strictly parse the manifest, match its tag and
version to the request and repository to the constant above, and require
`source_commit` to match the certificate's GitHub workflow SHA extension.
For CLI verification, repeat the command with
`--certificate-github-workflow-sha "$SOURCE_COMMIT"`, using the now
authenticated, validated manifest value. The producer enforces this claim
against `GITHUB_SHA` before publishing. A future maintained Go verifier should
enforce all claims in one policy, including the authorized workflow identity.

Then verify archive basename, format, size and digest, safely extract only
the expected regular executable (reject traversal, links, duplicate members),
and verify its size/digest before executing it. Rehash cached executables on
every reuse, storing the manifest and bundle as offline evidence. Never fall
back to unsigned checksums, `latest`, or unverified legacy releases on failure.
No per-version pins are needed for future releases meeting this policy.

## Required repository administration (not automated here)

Before the first production release using this workflow, an authorized admin
should explicitly approve and perform:

1. **Settings → General → Releases → Enable release immutability** for
   `bruin-data/ingestr`. Confirm the first published release reports
   `immutable: true` through the GitHub releases API. This protects published
   assets and the associated tag; it does not apply retroactively.
2. **Settings → Rules → Rulesets → New tag ruleset**, targeting `v*`: restrict
   creation to authorized release maintainers/automation, restrict updates,
   and restrict deletions. Keep the bypass list minimal and reviewed. Protect
   the release workflow and its helper scripts through the main-branch review
   policy (including code-owner review if configured).
3. Approve a real new-version release to exercise GitHub OIDC, all native
   runners, draft upload/readback, and immutable publication. Check the
   workflow SAN, issuer, tag/ref, source SHA, signing-time evidence, all eight
   assets, and offline verification from a separate machine. Do not use an
   existing version for a rebuild.

Without these settings the workflow refuses overwrites, but another privileged
actor can still move tags, change/delete releases, or mint a second valid
signature for the same tag. A failed draft needs explicit maintainer recovery;
never delete a published release to reuse its version. Signatures authenticate
publisher-authorized bytes, **not** reproducibility or proof that a binary
was built faithfully from source. They do not cover dynamically installed
ADBC drivers, system libraries, or compromised authorized workflows/runners.

## Local checks

```bash
python3 -m unittest discover -s hack -p 'test_release*.py' -v
bash -n hack/publish-release.sh
actionlint -shellcheck='' -ignore 'label "depot-' .github/workflows/release.yml .github/workflows/tests.yml
goreleaser check
make format && make lint && make test
```

Offline contract tests use synthetic archives for all five platforms. They
exercise deterministic output, completeness (including missing Windows),
malformed/duplicate fields, tag/SHA consistency, archive/executable tampering,
unsafe members, and exact version smoke checks. Mocked GitHub and Cosign CLIs
exercise the actual draft publisher, including signature/API/upload failures,
moved tags, existing releases, missing Windows, and changed downloaded bytes;
none may publish.
Actionlint's exception recognizes the repository's existing custom Depot
runner labels; shell syntax is checked separately. These tests do not exercise
production GitHub OIDC or publish a release.

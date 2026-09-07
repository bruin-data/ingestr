# Authenticated releases

Release signing uses the commit-pinned official [actions/attest](https://github.com/actions/attest)
action, with GitHub OIDC and Sigstore. No separate Cosign installation or custom
publisher is needed. Public-installer and Bruin verification remain follow-up work.

## Production flow

1. Accept only stable `vMAJOR.MINOR.PATCH` tags. Refuse existing releases/drafts.
2. Build all five archives with GoReleaser, using same-run/run-attempt artifacts
   rather than commit-keyed caches. Rerun **all jobs** after a failed attempt.
3. Run each packaged executable natively, requiring `ingestr version <tag>`.
   Integration tests and both native Docker image tests also gate publication.
4. Generate the manifest and compatibility checksums from the actual archives.
5. Attest the manifest with `actions/attest`, copy its `bundle-path` output into
   the release assets, and verify workflow identity, tag/ref and source SHA
   using `gh attestation verify`.
6. Use `gh release create TAG FILES...` to upload everything into an internal
   draft and publish once, marking latest only with the complete asset set.
   GitHub CLI owns upload failure handling (including temporary-draft cleanup).
   Existing published versions are never overwritten. Release immutability
   locks the published assets/tag and provides GitHub's release attestation.

Public-installer tests run afterward because they require public download URLs.
They cannot consume a private draft. The pre-publication native checks test all
five actual packaged executables instead.

## Manifest v1

The only custom release tool, `hack/release_manifest.py`, records information
official attestation actions do not extract: executable members inside archives.
It uses Python's standard library only and is not shipped with ingestr.

`ingestr-manifest.v1.json` contains `schema_version: 1`,
`repository: "bruin-data/ingestr"`, canonical `tag`, `version` without `v`, full
40-character `source_commit`, and exactly five `platforms` entries:
Darwin/Linux amd64 and arm64, plus Windows amd64. Each entry has `goos`, `goarch`,
`archive: {name, format, size, sha256}` and `executable: {path, size, sha256}`.
Executable paths are exactly `ingestr` or `ingestr.exe`; formats are `tar.gz`
or `zip`; sizes are positive byte counts and hashes are lowercase SHA-256 hex.

The eight release assets are the five archives, `ingestr_<version>_checksums.txt`
(including Windows), the manifest, and `ingestr-manifest.v1.sigstore.json`.
The bundle contains a signed SLSA provenance statement whose subject digest
authenticates the **exact manifest bytes**; it is not a raw `cosign sign-blob`
signature. Do not reserialize the manifest before verifying it.

## Future installer verification

Use a trusted GitHub CLI/verifier, never one obtained from the unverified release.
For the user-requested tag, verify the downloaded manifest and bundle:

```bash
gh attestation verify ingestr-manifest.v1.json \
  --bundle ingestr-manifest.v1.sigstore.json --repo bruin-data/ingestr \
  --cert-identity "https://github.com/bruin-data/ingestr/.github/workflows/release.yml@refs/tags/$TAG" \
  --source-ref "refs/tags/$TAG"
```

The default issuer is `https://token.actions.githubusercontent.com` and the
default predicate is SLSA provenance v1. Strictly parse the authenticated
manifest, reject duplicate/unknown fields and missing/duplicate platforms,
match repository/tag/version to policy, then repeat verification with
`--source-digest "$SOURCE_COMMIT"` from the validated manifest. Verify archive
and extracted executable sizes/hashes before execution; rehash cached binaries
on every use. Never fall back to unsigned checksums or `latest` on failure.

For offline use, retain the bundle and independently trusted roots with the
installer; pass `--custom-trusted-root trusted_root.jsonl`. Obtain roots through
GitHub CLI's authenticated trust-root flow (`gh attestation trusted-root`), not
from the release being verified. Maintain reviewed verifier/root updates and
historical keys. Verification must retain transparency/signing-time checks:
the certificate must have been valid at the verified signing time, not merely
at today's wall clock. Offline verification cannot provide fresh revocation.

## Operations and checks

Keep release immutability enabled and `v*` tags protected against unauthorized
creation, updates and deletion. Protect workflow/helper changes through review.
Fix published binaries with a new version, never by replacing an old asset.
Signatures attest publisher-authorized bytes, **not** source reproducibility;
they do not cover runtime-downloaded drivers or compromised authorized workflows.

Local checks: `python3 -m unittest discover -s hack -p 'test_release*.py' -v`,
actionlint (allow the existing Depot runner labels), `goreleaser check`, and
`make format && make lint && make test`. No production release is needed for
these checks. Actual GitHub OIDC and immutable publication must be verified on
the next authorized fresh-version release, including offline bundle verification.

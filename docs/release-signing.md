# Immutable releases

ingestr uses [GitHub immutable releases](https://docs.github.com/en/code-security/concepts/supply-chain-security/immutable-releases).
GitHub automatically signs a release attestation binding its repository, tag,
commit and asset digests. No custom manifest, signing script or attestation action
is needed. Keep release immutability enabled and version tags protected.

The release workflow builds all five platform archives using same-run artifacts,
smoke-tests each packaged executable against the exact tag, and waits for
integration and Docker tests. It generates compatibility checksums including
Windows, then uses `gh release create TAG FILES...` to upload all six assets to
an internal draft before publishing once and marking latest. Existing releases
are refused. Rerun **all jobs** after a failed attempt; fix published binaries
with a new version, not replacement assets.

After publication, the workflow verifies the exact release attestation and all
five local archives plus the checksum file using GitHub CLI. A verification
failure fails the job and blocks downstream jobs, but the release is already
public and marked latest. Investigate rather than rerunning publication for that
tag. No propagation retries are assumed; add bounded retries only if needed.

## Verification

With a trusted, current GitHub CLI, verify the requested release and downloaded
archive before extracting or executing it:

```bash
TAG=v1.2.3 # the exact user-requested version
gh release verify "$TAG" --repo bruin-data/ingestr
gh release verify-asset "$TAG" ./ingestr_Linux_x86_64.tar.gz --repo bruin-data/ingestr
```

This authenticates membership in the repository's exact immutable release, not
a particular build workflow or source reproducibility. Old mutable releases do
not gain attestations retroactively. Never fall back to unsigned checksums or
`latest` on verification failure. Future installers should retain the verified
archive and reverify/re-extract it for caching rather than trust a cached binary
without checking it. Public-installer and Bruin verification remain separate work.

Native archive tests gate publication; public-installer tests run afterward
because they require public release URLs. Confirm immutability and both CLI
verification commands on the next authorized fresh-version release. Local
workflow checks cannot exercise GitHub's publication/signing service.

# Licensing Workflow

ingestr uses three separate license checks. CI runs the first two on PRs:

- `make licenses-check` is the fast policy gate. It runs `go-licenses check`
  against the canonical release target and fails on disallowed license types.
- `make licenses-audit` is the review gate. It checks `licenses.lock.yml` and
  fails when a scanned dependency, version, or license changes without an
  explicit audit update.
- `make licenses` regenerates `THIRD_PARTY_LICENSES.txt` for release notices.

## Review Policy

Use these statuses in `licenses.lock.yml`:

- `allowed`: permissive or already accepted license metadata.
- `manual-review`: accepted after human review, usually because the scanner
  cannot classify the license file or the dependency has special obligations.
- `needs-review`: generated placeholder for new or changed dependencies. Do not
  merge with this status.
- `blocked`: dependency must not be used.

Typical default-allowed licenses are MIT, BSD, Apache-2.0, ISC, and similar
permissive licenses. Unknown, custom, GPL, AGPL, LGPL, CDDL, or proprietary
licenses need manual review before use.

## Updating The Audit Lock

When `go.mod` or `go.sum` changes:

```bash
make licenses-audit
```

If it fails because dependencies changed, regenerate the lock:

```bash
make licenses-audit-update
```

New or changed entries are written with `status: needs-review`. Review each one,
then set the status to `allowed`, `manual-review`, or `blocked` with a short
note when the decision is not obvious.

Manual license pins live under `manual_audits` in `licenses.lock.yml`. The
notice generator validates their selected version and license file SHA before
including them in `THIRD_PARTY_LICENSES.txt`.

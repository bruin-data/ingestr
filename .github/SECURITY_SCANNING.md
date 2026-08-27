# Codex Security daily scan

[`.github/workflows/codex-security.yml`](workflows/codex-security.yml) runs a full
[Codex Security](https://developers.openai.com/codex/security) scan of the default
branch once a day and publishes the results to GitHub Code Scanning.

## At a glance

| | |
| --- | --- |
| Schedule | Daily at 04:27 UTC, plus **Actions → codex_security → Run workflow** |
| Scope | The whole working tree of the default branch. No PR, push, or diff scans |
| Secret | `OPENAI_API_KEY` (repository or organization Actions secret) |
| Findings | **Security → Code scanning**, filtered by tool `Codex Security` |
| Cost ceiling | `CODEX_SECURITY_MAX_COST` in the workflow (default `20` USD per scan) |
| Job timeout | 180 minutes |

## Where findings go

Code Scanning is the findings inbox. The workflow does not open issues, comment on
pull requests, or block merges. Use the Security tab to read a finding, jump to its
source location, and dismiss it as a false positive or accepted risk.

Alerts are uploaded under the category `codex-security` against
`refs/heads/<default-branch>`. GitHub matches results across uploads by source-line
fingerprint, so re-running the scan updates existing alerts rather than duplicating
them, a fixed finding is resolved by the next scan, and a dismissal survives later
uploads.

Code scanning uploads require GitHub Advanced Security to be enabled on private
repositories.

## What fails the job, and what does not

A vulnerability is not a build failure. The scan runs report-only — no
`--fail-on-severity` — so a completed scan exits `0` no matter what it found.

The job fails only on operational problems: a missing or rejected `OPENAI_API_KEY`,
a scanner crash, an interrupted run, a failed SARIF export, or a failed upload.

Incomplete coverage also fails the job, and in that case **nothing is uploaded**.
This is deliberate: GitHub resolves alerts that a new upload no longer reports, so
publishing the partial results of a scan that stopped early would silently close
real alerts the scanner never got around to re-finding. The most likely cause is the
scan hitting its cost ceiling; raise `CODEX_SECURITY_MAX_COST` and re-run.

## Handling scan output

Scan state, artifacts, and the exported SARIF are written to `/tmp/codex-security`,
outside the checkout, so findings and source snippets cannot end up as repository
files. The scanner also refuses an output directory inside the scanned git worktree.

The uploaded SARIF is the only durable output. The workflow intentionally does not
upload build artifacts: the scan directory contains vulnerability details and source
snippets. The job summary reports finding counts by severity only.

To debug a failing scan, start with the step logs, then add `--verbose` to the scan
command on a branch. If you need the raw artifacts, add a temporary
`actions/upload-artifact` step with `retention-days: 1` and remove it afterwards.

## Reusing this in another repository

1. Copy `.github/workflows/codex-security.yml` and merge it to the default branch —
   `schedule` and `workflow_dispatch` only take effect from there.
2. Make `OPENAI_API_KEY` available to the repository.
3. Enable code scanning (GitHub Advanced Security on private repositories).
4. Dispatch the workflow manually a few times before relying on the schedule, and
   check runtime and cost against `CODEX_SECURITY_MAX_COST`.

Bump `CODEX_SECURITY_VERSION` to pick up a new `@openai/codex-security` release.

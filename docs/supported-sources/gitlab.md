# GitLab

[GitLab](https://gitlab.com/) is a DevOps platform for hosting Git repositories, managing issues and merge requests, and running CI/CD pipelines.

ingestr supports GitLab as a source through its REST API.

## URI format

The URI format for GitLab is as follows:

```plaintext
gitlab://?access_token=<access_token>
```

URI parameters:

- `access_token` (required): A GitLab personal access token used to authenticate with the REST API.
- `base_url` (optional): The API base URL for self-managed GitLab instances, including the `/api/v4` suffix (e.g. `https://gitlab.example.com/api/v4`). Defaults to `https://gitlab.com/api/v4`.

## Setting up a GitLab Integration

To connect to GitLab, you need a personal access token (PAT).

1. Log in to [GitLab](https://gitlab.com/).
2. Select your avatar in the upper-right corner → **Edit profile**.
3. In the left sidebar, select **Access** → **Personal access tokens**.
4. From the **Generate token** dropdown, select **Legacy token**.
5. Enter a token name and an expiration date, then select the `read_api` scope (sufficient for read-only ingestion).
6. Select **Generate token** and copy the value (starts with `glpat-`).

For details, see the [GitLab personal access tokens documentation](https://docs.gitlab.com/user/profile/personal_access_tokens/).

Once you have your access token, let's say it is `glpat-1234`, here is a sample command that copies issues from GitLab into a DuckDB database:

```sh
ingestr ingest \
  --source-uri 'gitlab://?access_token=glpat-1234' \
  --source-table 'issues' \
  --dest-uri duckdb:///gitlab.duckdb \
  --dest-table 'dest.issues'
```

## Tables

GitLab source allows ingesting the following resources into separate tables:

| Table            | PK   | Inc Key      | Inc Strategy | Details                                                                                       |
| ---------------- | ---- | ------------ | ------------ | --------------------------------------------------------------------------------------------- |
| `projects`       | `id` | `updated_at` | merge        | Projects the token is a member of. Scoped to the run interval via `updated_after`/`updated_before`. |
| `groups`         | `id` | –            | replace      | Groups the token is a member of. Full reload on each run.                                      |
| `users`          | `id` | –            | replace      | Users across the projects the token is a member of. Full reload on each run. |
| `issues`         | `id` | `updated_at` | merge        | All issues across the projects the token is a member of. Scoped to the run interval via `updated_after`/`updated_before`. |
| `merge_requests` | `id` | `updated_at` | merge        | All merge requests across the projects the token is a member of. Scoped to the run interval via `updated_after`/`updated_before`. |

Use these as the `--source-table` parameter in the `ingestr ingest` command.

> **Note**: GitLab objects carry both a global `id` and a project-scoped `iid`. ingestr keys on the global `id`. Nested fields such as `labels`, `assignees`, and `references` are preserved as JSON, and `description` fields remain Markdown strings.
### Incremental loads

`projects`, `issues`, and `merge_requests` support incremental loading. Provide `--interval-start` (and optionally `--interval-end`) and ingestr pushes them to the GitLab API as `updated_after`/`updated_before`, fetching only records updated within the window and merging them on `id`.

```sh
ingestr ingest \
  --source-uri 'gitlab://?access_token=glpat-1234' \
  --source-table 'merge_requests' \
  --dest-uri duckdb:///gitlab.duckdb \
  --dest-table 'dest.merge_requests' \
  --interval-start '2026-01-01'
```

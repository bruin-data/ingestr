# Jira

[Jira](https://www.atlassian.com/software/jira) is a proprietary issue tracking product developed by Atlassian that allows bug tracking and agile project management.

ingestr supports Jira as a source through [Jira's REST API v3](https://developer.atlassian.com/cloud/jira/platform/rest/v3/).

## URI format

The URI format for Jira is:

```plaintext
jira://your-domain.atlassian.net?email=<email>&api_token=<api_token>
```

URI parameters:
- `email`: the email address used for authentication with the Jira API
- `api_token`: the API token for authentication (required for Jira Cloud)

## Example usage

Assuming your Jira domain is `company.atlassian.net`, email is `user@company.com`, and API token is `ATATT3xFfGF0...`, you can ingest issues into DuckDB using:

```bash
ingestr ingest \
  --source-uri 'jira://company.atlassian.net?email=user@company.com&api_token=ATATT3xFfGF0...' \
  --source-table 'issues' \
  --dest-uri duckdb:///jira.duckdb \
  --dest-table 'dest.issues'
```

## Authentication

To connect to Jira, you need:

1. **Email**: Your Atlassian account email
2. **API Token**: Create one from your [Atlassian Account Settings](https://id.atlassian.com/manage-profile/security/api-tokens)

## Tables

Jira source allows ingesting the following tables:

| Table | PK | Inc Key | Inc Strategy | Details |
| ----- | -- | ------- | ------------ | ------- |
| `projects` | id | - | replace | Fetches all projects from your Jira instance. |
| `issues` | id | fields.updated | merge | Fetches all issues with support for incremental loading based on updated timestamp. |
| `users` | accountId | - | replace | Fetches users from your Jira instance. |
| `issue_types` | id | - | replace | Fetches all issue types configured in your Jira instance. |
| `statuses` | id | - | replace | Fetches all workflow statuses from your Jira instance. |
| `priorities` | id | - | replace | Fetches all issue priorities from your Jira instance. |
| `resolutions` | id | - | replace | Fetches all issue resolutions from your Jira instance. |
| `project_versions` | id | - | replace | Fetches versions for each project. |
| `project_components` | id | - | replace | Fetches components for each project. |

Use these as the `--source-table` parameter in the `ingestr ingest` command.

## Filtering archived projects

`projects`, `project_versions` and `project_components` can be suffixed with `:skip_archived` to filter out archived projects.

For instance:
```bash
ingestr ingest \
  --source-uri 'jira://company.atlassian.net?email=user@company.com&api_token=ATATT3xFfGF0...' \
  --source-table 'project_versions:skip_archived' \
  --dest-uri duckdb:///jira.duckdb \
  --dest-table 'dest.live_project_versions'
```

Will only load versions of non-archived projects.

## Incremental Loading

The `issues` table supports incremental loading based on the `updated` field. This means subsequent runs will only fetch issues that have been modified since the last run, making the data ingestion more efficient for large Jira instances.

> [!NOTE]
> Most tables use "replace" write disposition, meaning they will overwrite existing data on each run. Only the `issues` table supports incremental loading with "merge" disposition.
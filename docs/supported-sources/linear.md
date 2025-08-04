# Linear

[Linear](https://linear.app/) is a purpose-built tool for planning and building products that provides issue tracking and project management for teams.

ingestr supports Linear as a source.

## URI format

The URI format for Linear is:

```plaintext
linear://?api_key=<api_key>
```

URI parameters:

- `api_key`: The API key used for authentication with the Linear API.

## Example usage

Assuming your API key is `lin_api_123`, you can ingest teams into DuckDB using:

```bash
ingestr ingest
--source-uri 'linear://?api_key=lin_api_123' \
--source-table 'teams' \
--dest-uri duckdb:///linear.duckdb \
--dest-table 'dest.teams'
```
<img alt="linear" src="../media/linear.png"/>

## Tables
Linear source allows ingesting the following tables:

- `issues`: Fetches all issues from your Linear workspace.
- `projects`: Fetches project-level data.
- `teams`: Fetches information about the teams configured in Linear.
- `users`: Fetches users from your workspace.
- `workflow_states`: Fetches workflow states used in your Linear workspace.
- `cycles`: Fetches cycle information and planning data.
- `attachments`: Fetches file attachments associated with issues.
- `comments`: Fetches comments on issues and other entities.
- `documents`: Fetches documents created in Linear.
- `external_users`: Fetches information about external users.
- `initiative`: Fetches initiative data for high-level planning.
- `integrations`: Fetches integration configurations.
- `labels`: Fetches labels used for categorizing issues.
- `organization`: Fetches organization-level information.
- `project_updates`: Fetches updates posted to projects.
- `roadmaps`: Fetches roadmap data for strategic planning.
- `roadmap_to_projects`: Fetches relationships between roadmaps and projects.
- `team_memberships`: Fetches team membership information.
- `initiative_to_project`: Fetches relationships between initiatives and projects.
- `project_milestone`: Fetches project milestone data.
- `project_status`: Fetches project status information.
- `team`: Fetches team-level data.
- `project`: Fetches individual project information.


Use these as the `--source-table` parameter in the `ingestr ingest` command.

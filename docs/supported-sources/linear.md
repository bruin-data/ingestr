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
Linear source allows ingesting the following tables::

- `issues`: Fetches all issues from your Linear workspace.
- `projects`: Fetches project-level data, .
- `teams`: Fetches information about the teams configured in Linear.
- `users`: Fetches users from your workspace.


Use these as the `--source-table` parameter in the `ingestr ingest` command.

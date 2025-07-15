# ClickUp
[ClickUp](https://clickup.com/) is a productivity platform for teams.

ingestr supports ClickUp as a source.

## URI format

```
clickup://?api_token=<token>
```

URI parameters:
- `api_token` is a personal token used to authenticate with the ClickUp API.

ClickUp requires a `api_token` to connect to the ClickUP API. For more information, read [here](https://developer.clickup.com/docs/authentication#generate-your-personal-api-token). Once you've completed the guide, you should have `api_token`. Let's say your API Token  is `token_123`, here's a sample command that will copy the data from Clickup into a DuckDB database:

## Example

To ingest tasks from ClickUp into a DuckDB database:

```sh
ingestr ingest \
  --source-uri "clickup://?api_token=token_123" \
  --source-table "tasks" \
  --dest-uri duckdb:///clickup.duckdb \
  --dest-table "public.tasks"
```

## Tables

The ClickUp source exposes the following tables:

| **Table** | **Description** |
|-----------|-----------------|
| `users`   | The authorised user profile. |
| `teams`   | Teams accessible to the token. |
| `spaces`  | Spaces within each team. |
| `lists`   | Lists contained in each space. |
| `tasks`   | Tasks belonging to each list. |

Use these as `--source-table` parameter in the `ingestr ingest` command.

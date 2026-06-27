# Monday.com
[Monday.com](https://monday.com/) is a Work OS that powers teams to run projects and workflows with confidence. It's a simple, yet powerful platform that enables people to manage work, meet deadlines, and build a culture of transparency.

## URI format

The URI format for Monday.com is as follows:
```
monday://?api_token=<api_token>
```

URI parameters:
- `api_token` is your Monday.com API token for authentication.

## Setting up a Monday.com Integration

You can obtain an API token from the [Monday.com Developer Console](https://developer.monday.com/). For more information, see [Monday.com authentication documentation](https://developer.monday.com/api-reference/docs/authentication).

To get your API token:
1. Go to your Monday.com account
2. Click on your profile picture in the top right corner
3. Select "Admin" → "API"
4. Generate a new API token

## Example
Let's say you want to ingest all boards into a DuckDB database called `monday.db`. For this example the value of `api_token` will be `fake_token`.

You can run the following to achieve this:
```sh
ingestr ingest \
  --source-uri "monday://?api_token=fake_token" \
  --source-table "boards" \
  --dest-uri "duckdb://./monday.db" \
  --dest-table "public.boards"
```

## Tables

Monday.com source allows ingesting the following resources into separate tables:

| Table | Primary/Merge Key | Inc Key | Inc Strategy | Details |
|-------|-------------------|---------|--------------|---------|
| `account` | - | - | replace | Account information including name, slug, tier, and plan details. Full reload on each run. |
| `account_roles` | - | - | replace | Account roles with their types and permissions. Full reload on each run. |
| `users` | - | - | replace | Users in your Monday.com account with their profiles and permissions. Full reload on each run. |
| `boards` | id | updated_at | merge | Boards with their metadata, state, and configuration. Incrementally loads only updated boards. |
| `items` | id | - | replace | Items (rows) across all boards, including each cell's raw values as a JSON array in `column_values`. Full reload on each run. |
| `workspaces` | - | - | replace | Workspaces containing boards and their settings. Full reload on each run. |
| `webhooks` | - | - | replace | Webhooks configured for boards with their events and configurations. Full reload on each run. |
| `updates` | id | updated_at | merge | Updates (comments) on items with their content and metadata, including the creator and item names. Incrementally loads only updated entries. |
| `teams` | - | - | replace | Teams in your account with their members. Full reload on each run. |
| `tags` | - | - | replace | Tags used across your account for organizing items. Full reload on each run. |
| `custom_activities` | - | - | replace | Custom activity types defined in your account. Full reload on each run. |
| `board_columns` | - | - | replace | Columns defined in all boards with their types and settings. Full reload on each run. |
| `board_views` | - | - | replace | Views configured for boards with their filters and settings. Full reload on each run. |

Use these as `--source-table` parameter in the `ingestr ingest` command.

### Scoping to specific boards

`items`, `boards`, `board_columns`, `board_views`, and `updates` accept an optional `:<board_id>[,<board_id>...]` suffix to restrict the result to the listed boards. Without a suffix they behave as before (every board in the account).

```sh
# Items on a single board
ingestr ingest --source-table "items:5091839751" ...

# board_columns from two boards
ingestr ingest --source-table "board_columns:5091839751,5091841857" ...

# Updates on items belonging to a single board
ingestr ingest --source-table "updates:5091841883" ...
```

### `items:<board_id>:linked`

`items` additionally supports a `:linked` suffix that treats the given board as a "master" board and also pulls items from any **sub-boards whose name matches one of the master's item titles**. Useful for a "master board → linked sub-boards" fan-out pattern where the master's items name the sub-boards. Requires at least one master board id.

```sh
ingestr ingest --source-table "items:5091839751:linked" ...
```

> [!NOTE]
> `:linked` discovers sub-boards by the convention that **an item on the master board has the same title as the sub-board's name**. So if your master board has an item called `Q1 Roadmap` and you also have a board called `Q1 Roadmap`, that board is treated as a linked sub-board and its items are included in the result.

> [!NOTE]
> Monday.com rate limits API requests, mainly by query complexity per minute. The source paginates automatically (up to the API's 100-item page size), throttles its own request rate, and retries throttled (HTTP 429) and transient (5xx) responses. It waits the time Monday asks for, whether from the `Retry-After` header or the `retry_in_seconds` field a complexity error returns instead.


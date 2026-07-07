# Trello
[Trello](https://trello.com/) is a visual work-management tool that organizes projects into boards, lists, and cards.

ingestr supports Trello as a source.

## URI format

```
trello://?api_key=<api_key>&token=<token>
```

URI parameters:
- `api_key`: the API key of a Trello Power-Up, used to identify your application.
- `token`: a token that authorizes access to your Trello account's data.

Trello requires both an `api_key` and a `token`. Create a Power-Up to obtain an API key, then generate a token for your own account. See Trello's [API key and token guide](https://developer.atlassian.com/cloud/trello/guides/rest-api/api-introduction/) for the steps.

Once you have both values, here's a sample command that copies boards from Trello into a DuckDB database:

```sh
ingestr ingest \
  --source-uri "trello://?api_key=key_123&token=token_123" \
  --source-table "boards" \
  --dest-uri duckdb:///trello.duckdb \
  --dest-table "public.boards"
```

## Tables

Trello source allows ingesting the following resources into separate tables:

| Table         | PK | Inc Key            | Inc Strategy | Details                                                              |
| ------------- | -- | ------------------ | ------------ | ------------------------------------------------------------------- |
| boards        | id | –                  | replace      | All boards the authenticated member can access                      |
| organizations | id | –                  | replace      | Workspaces (organizations) the member belongs to                    |
| lists         | id | –                  | replace      | Lists across all accessible boards                                  |
| members       | id | –                  | replace      | Members across all accessible boards, de-duplicated                 |
| labels        | id | –                  | replace      | Labels defined on each board                                        |
| checklists    | id | –                  | replace      | Checklists across all accessible boards                             |
| cards         | id | `dateLastActivity` | merge        | Cards across all accessible boards                                  |
| actions       | id | `date`             | merge        | Activity log (actions) across all accessible boards                 |

Use these as the `--source-table` parameter in the `ingestr ingest` command.

By default the board-scoped tables (`lists`, `members`, `labels`, `checklists`, `cards`, `actions`) fetch data from **every board the account can access**. To scope a run to specific boards, append a comma-separated list of board IDs after the table name with a colon:

```sh
# only these two boards
--source-table "cards:5f2a…,60b1…"
```

The `boards` and `organizations` tables don't accept a board filter.

## Incremental loading

When `--interval-start` / `--interval-end` are provided, `cards` and `actions` are loaded incrementally by their most recent activity and merged on `id`; the other tables are fully refreshed on each run. When no interval is provided, all records are fetched.

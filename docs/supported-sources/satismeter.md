# SatisMeter

[SatisMeter](https://www.satismeter.com/) collects in-app NPS, CSAT and CES survey
responses.

ingestr supports SatisMeter as a source.

## URI format

```
satismeter://?api_key=<api_key>&project_id=<project_id>
```

Both parameters are required:

- `api_key` — created in the SatisMeter UI under **Settings → Integrations → API**.
  The key is scoped to a single project.
- `project_id` — the project the key belongs to. Every endpoint is nested under
  `/projects/{projectId}`, so it cannot be inferred from the key.

## Tables

| Table       | Description                                                                                   |
|-------------|-----------------------------------------------------------------------------------------------|
| `responses` | Individual survey responses: the answer payload, the responding user, device, location, language, referrer and a `created` timestamp. |
| `campaigns` | The surveys defined in the project — id, name, type (`nps` / `csat` / …) and state.             |
| `project`   | Project metadata: name, default language, branding.                                            |

`responses` is loaded incrementally on its `created` timestamp. `campaigns` and
`project` are small snapshots that are re-fetched in full and de-duplicated on `id`;
neither tracks deletions, so a survey removed in SatisMeter remains in the table.

Nested objects (`answers`, `user`, `device`, `location`) are passed through as JSON
rather than flattened.

### A note on date ranges

SatisMeter's responses endpoint defaults to **the last 30 days** when no start date
is supplied. ingestr always sends an explicit start date, so a run without
`--interval-start` fetches the full history rather than silently returning a month.
Pass `--interval-start` / `--interval-end` to narrow it.

### Personal data

Response records embed the respondent's email, name, user id and the full custom
trait bag exactly as SatisMeter returns them. Treat the destination table as
containing personal data.

## Example

```sh
ingestr ingest \
  --source-uri 'satismeter://?api_key=sm_abc123&project_id=5bb480aaebf3ed0004c6f3dd' \
  --source-table 'responses' \
  --dest-uri duckdb:///satismeter.duckdb \
  --dest-table 'dest.responses'
```

<img alt="satismeter" src="../media/satismeter.png" />

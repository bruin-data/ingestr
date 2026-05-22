# Granola

[Granola](https://www.granola.ai/) is an AI meeting-notes product. Ingestr supports Granola as a source for notes and folders through the Granola public API.

## URI Format

```plaintext
granola://?api_key=<api-key>
```

URI parameters:

- `api_key` (required): Granola API token used as a bearer token.

## Example

```sh
ingestr ingest \
  --source-uri 'granola://?api_key=your-api-key' \
  --source-table 'notes' \
  --dest-uri duckdb:///granola.duckdb \
  --dest-table 'main.notes'
```

## Tables

| Table | PK | Inc Key | Inc Strategy | Details |
| ----- | -- | ------- | ------------ | ------- |
| `notes` | `id` | `updated_at` | `merge` | Meeting notes listed from `/v1/notes` and hydrated from `/v1/notes/{note_id}`, including summary, transcript, attendees, folder membership, calendar event, and web URL. Incremental runs use Granola's `updated_after` parameter. |
| `folders` | `id` | - | `replace` | Accessible folders returned by the `/v1/folders` endpoint. |

## Incremental Loading

The `notes` table supports incremental loading by `updated_at`.

```sh
ingestr ingest \
  --source-uri 'granola://?api_key=your-api-key' \
  --source-table 'notes' \
  --dest-uri duckdb:///granola.duckdb \
  --dest-table 'main.notes' \
  --interval-start '2026-01-01T00:00:00Z'
```

The `folders` table is loaded as a full refresh.

The `notes` table fetches note details with `include=transcript`, so transcripts are available in the `transcript` JSON column when Granola returns them.

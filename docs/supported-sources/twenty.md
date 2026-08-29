# Twenty CRM

[Twenty](https://twenty.com/) is an open-source CRM. It runs both as a hosted
workspace (`api.twenty.com`) and self-hosted on your own domain; ingestr supports
both.

ingestr supports Twenty CRM as a source.

## URI format

```
twenty://<host>?api_key=<api_key>
```

- `host` — the workspace host. `api.twenty.com` for Twenty Cloud, or your own
  domain for a self-hosted instance (e.g. `crm.example.com`).
- `api_key` — created in the workspace under **Settings → API & Webhooks**. The
  key is shown only once. One key covers one workspace.

Optional parameters:

- `page_size` — rows per request, default and maximum `200`.
- `rate_limit` — requests per second, default `1.33` (80% of Twenty's documented
  100 requests/minute).
- `include_deleted` — default `true`, see below.
- `base_path` — where the REST API is mounted, default `/rest`.

Example:

```sh
ingestr ingest \
  --source-uri 'twenty://api.twenty.com?api_key=eyJ...' \
  --source-table 'people' \
  --dest-uri $DEST \
  --dest-table 'main.people'
```

## Tables

Twenty supports the following tables:

| Table | PK | Inc Key | Inc Strategy | Details |
|-------|----|---------|--------------|---------|
| `companies` | id | updatedAt | merge | Companies in the workspace. |
| `notes` | id | updatedAt | merge | Notes attached to workspace records. |
| `opportunities` | id | updatedAt | merge | Sales opportunities. |
| `people` | id | updatedAt | merge | People in the workspace. |
| `tasks` | id | updatedAt | merge | Tasks attached to workspace records. |
| `workspaceMembers` | id | updatedAt | merge | Members of the workspace. |
| `custom:<object_name>` | id | updatedAt | merge | A custom object, using its plural API name. |

Twenty exposes custom objects through the same REST API as standard objects. To
ingest one, prefix its plural API name with `custom:`, for example
`custom:leads`. The connector reads the object's metadata at runtime, so custom
fields are included automatically.

```sh
ingestr ingest \
  --source-uri 'twenty://api.twenty.com?api_key=eyJ...' \
  --source-table 'custom:leads' \
  --dest-uri $DEST \
  --dest-table 'main.leads'
```

## Columns

Columns follow Twenty's field names verbatim.

- **Relations become foreign keys.** A person's `company` relation lands as the
  `companyId` column, matching what the API returns. One-to-many relations
  (a person's `noteTargets`) produce no column — that key lives on the child.
- **Composite fields use the JSON type** rather than being flattened:
  `name`, `emails`, `phones`, `address`, `domainName`, `linkedinLink`, `amount`,
  `createdBy`, `updatedBy`.
- **Money is not a float.** Twenty stores currency as
  `{"amountMicros": 1500000, "currencyCode": "CZK"}` — integer micros. The digits
  are carried through exactly; divide by 1e6 when you model it.
- `searchVector` is dropped. It is Postgres' full-text index, not data.

## Incremental loading

Every Twenty object carries `updatedAt`, which is used as the incremental key with
the `merge` strategy, filtered server-side:

```
filter=updatedAt[gte]:2026-08-01T00:00:00.000Z
```

Only the start of the interval is applied. An upper bound would make re-running an
old window silently drop rows edited since, and `merge` is idempotent, so a wider
window costs requests rather than correctness.

Records are walked with Twenty's cursor pagination in the API's default order,
which is by `id`. Ordering by `updatedAt` instead would be unstable: a record
edited mid-run moves in the sort order and can be skipped or read twice across a
page boundary.

## Deleted records

Twenty soft-deletes: a deleted record keeps its row with `deletedAt` set, and is
**excluded from every list response by default**. Left at that, a record deleted
in the CRM would keep its last-known state in your warehouse forever, looking
live.

So by default ingestr makes a second pass with `deletedAt[is]:NOT_NULL` and
re-reads exactly those records, whose `deletedAt` then lands populated. Filter on
`deletedAt IS NULL` downstream to see the live set.

Set `include_deleted=false` to skip that pass — one fewer walk per run, at the
cost of never learning about a deletion.

## Rate limits

Twenty documents **100 requests per minute**. The default limiter runs at 80% of
that. With the maximum page size of 200 rows, a 68,000-row object is roughly 340
requests, or about four minutes.

# Sumble

[Sumble](https://sumble.com/) provides organization, people, and intent-signal data for go-to-market teams.

ingestr supports Sumble as a source through Sumble's v9 API.

## URI format

```text
sumble://?api_key=<api-key>
```

Create an API key from the API keys page in your Sumble account. The key is sent to Sumble as a bearer token.

The following command copies Sumble signals into DuckDB:

```sh
ingestr ingest \
  --source-uri 'sumble://?api_key=your-api-key' \
  --source-table 'signals' \
  --dest-uri 'duckdb:///sumble.duckdb' \
  --dest-table 'main.signals'
```

Sumble charges API credits according to the resource and number of records returned. Check your Sumble plan and credit balance before running large extracts.

## Tables

| Table | Primary key | Incremental key | Strategy | Data |
| --- | --- | --- | --- | --- |
| `organization_lists` | `id` | – | replace | Saved organization-list metadata, including deleted lists |
| `organization_list_organizations` | `_ingestr_id` | – | replace | Organizations in every saved organization list, including deleted lists |
| `contact_lists` | `id` | – | replace | Saved contact-list metadata |
| `contact_list_people` | `_ingestr_id` | – | replace | People in every saved contact list, including available contact information. Deleted lists are excluded — the contact-list endpoint has no `include_deleted` option |
| `signals` | `_ingestr_id` | `date` | merge | Intent signals visible to the authenticated account |
| `priority_signals` | `id` | `date` | merge | Curated priority signals and relevance feedback |
| `signal_configs` | `id` | – | replace | Signal configuration definitions visible to the authenticated account |

Nested Sumble objects and arrays are preserved as JSON columns.

## Table parameters

Tables accept optional filters appended to the table name as query parameters. Multiple values are comma-separated, e.g. `signals?organization_ids=12,34`.

| Table | Parameters |
| --- | --- |
| `organization_list_organizations` | `list_ids` — restrict to specific lists instead of every list |
| `contact_list_people` | `list_ids` — restrict to specific lists instead of every list |
| `signals` | `organization_ids`, `person_ids`, `signal_ids`, `technology_slugs`, `job_functions`, `priorities`, `account_list_ids`, `signal_config_ids` |
| `priority_signals` | `organization_ids`, `person_ids`, `signal_ids`, `job_post_ids`, `is_relevant` |
| `signal_configs` | `signal_config_ids`, `types`, `priorities` |

`priorities` accepts `high`, `medium`, or `low`. `is_relevant` accepts `true` or `false`. For `signal_configs`, `signal_config_ids` cannot be combined with `types` or `priorities`.

```sh
ingestr ingest \
  --source-uri 'sumble://?api_key=your-api-key' \
  --source-table 'signals?organization_ids=12,34&priorities=high' \
  --dest-uri 'duckdb:///sumble.duckdb' \
  --dest-table 'main.signals'
```

## Limits

`signals` and `priority_signals` are paged with Sumble's offset pagination, which stops at an offset of 10,000. Extracts larger than that are truncated with a warning — narrow them with the table parameters above. Sumble's `signals` endpoint also only returns signals from the last 60 days unless you look them up directly with `signal_ids`.

## Incremental loading

`signals` and `priority_signals` support interval-based loading on `date`. The Sumble API does not provide a server-side date range for these resources, so ingestr pages through the matching results and applies the interval locally. The interval start is inclusive and the interval end is exclusive.

`signals` is returned newest-first, so paging stops as soon as a page falls entirely before the interval start. This only bounds the old end of the window: an interval far in the past is still reached by paging back from the newest signal, so it costs a full extract in credits and usually hits the offset limit first. `priority_signals` is ordered by completion time rather than by `date`, so a narrow interval always pages through the full result set. Both endpoints charge one credit per record returned, whether or not the record survives the interval filter.

`priority_signals` carries a date-only `date`, so a row is kept whenever any part of its day falls in the interval. Rows whose `date` is missing or unparseable are always kept, which means they are re-fetched on every run.

`signals` rows are keyed on `signal_id`. Sumble leaves that field empty for some signals, and those rows fall back to a hash of the row itself: if such a signal later changes, the merge writes it as a new row rather than updating the existing one.

The remaining tables are replaced in full so removals from lists and changes to configuration are reflected in the destination.

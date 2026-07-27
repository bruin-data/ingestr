# OpenAI

[OpenAI](https://openai.com/) provides organization-level usage reporting for API traffic. The OpenAI source loads daily completion usage totals, including input, cached-input, cache-write, and output tokens where available.

## URI format

```plaintext
openai://?api_key=<admin-api-key>&codex_api_key=<codex-analytics-api-key>
```

The credentials are independent and either can be omitted when its API family is not used:

- `api_key` is an OpenAI organization Admin API key used by Platform organization usage tables.
- `codex_api_key` is the Platform organization API key associated with the ChatGPT workspace used by Codex Analytics tables.

Both values should be stored as secrets. If the same key has access to both API families, pass it explicitly in both parameters.

The currently available `api_usage` table uses only `api_key`. Codex Analytics tables will use only `codex_api_key` once their authenticated API contract is available.

## Examples

### Daily usage by user

Load the last 30 days of API usage into DuckDB. `api_usage` groups by `user_id` by default:

```sh
ingestr ingest \
  --source-uri 'openai://?api_key=<admin-api-key>' \
  --source-table 'api_usage' \
  --dest-uri 'duckdb:///openai.duckdb' \
  --dest-table 'openai.api_usage'
```

### Daily usage by user and API key

Use Monday-style table parameters to add grouping dimensions:

```sh
ingestr ingest \
  --source-uri 'openai://?api_key=<admin-api-key>' \
  --source-table 'api_usage?group_by=user_id,api_key_id' \
  --dest-uri 'duckdb:///openai.duckdb' \
  --dest-table 'openai.api_usage_by_key'
```

The equivalent repeated-parameter form is also supported:

```sh
--source-table 'api_usage?group_by=user_id&group_by=api_key_id'
```

### Daily usage by user, API key, and model

```sh
ingestr ingest \
  --source-uri 'openai://?api_key=<admin-api-key>' \
  --source-table 'api_usage?group_by=user_id,api_key_id,model' \
  --dest-uri 'duckdb:///openai.duckdb' \
  --dest-table 'openai.api_usage_by_key_model'
```

### A specific time range

```sh
ingestr ingest \
  --source-uri 'openai://?api_key=<admin-api-key>' \
  --source-table 'api_usage?group_by=project_id,model' \
  --dest-uri 'duckdb:///openai.duckdb' \
  --dest-table 'openai.api_usage_by_project' \
  --interval-start '2026-07-01T00:00:00Z' \
  --interval-end '2026-08-01T00:00:00Z'
```

### Separate credentials for both API families

When the Platform Usage and Codex Analytics APIs require different keys, include both in the same source URI:

```plaintext
openai://?api_key=<admin-api-key>&codex_api_key=<codex-analytics-api-key>
```

## Tables

| Table | Incremental key | Strategy | Description |
| --- | --- | --- | --- |
| `api_usage` | `bucket_start` | delete-insert | Daily API completion usage grouped by OpenAI organization user. |

`api_usage` supports `group_by` with one or more of: `project_id`, `user_id`, `api_key_id`, `model`, `batch`, and `service_tier`. Values may be comma-separated or supplied by repeating `group_by`. Unknown options and grouping fields return an error.

`user_id` identifies the OpenAI organization user associated with API usage. It does not necessarily identify an end user of your application. If multiple customers share a service account or API key, maintain customer attribution in your own application data.

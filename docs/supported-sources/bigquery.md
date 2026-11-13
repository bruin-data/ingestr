# Google BigQuery
BigQuery is a fully managed, serverless data warehouse that enables scalable analysis over petabytes of data.

ingestr supports BigQuery as both a source and destination.

## URI format
The URI format for BigQuery is as follows:

```plaintext
bigquery://<project-name>?credentials_path=/path/to/service/account.json&location=<location>
```

URI parameters:
- `project-name`: the name of the project in which the dataset resides
- `credentials_path`: optional, the path to the service account JSON file. If not provided, ingestr will use [Application Default Credentials](https://googleapis.dev/python/google-api-core/latest/auth.html#overview)
- `credentials_base64`: optional, base64-encoded service account JSON credentials
- `location`: optional, the location of the dataset
- `read_api`: optional, source only. How ingestr reads data from BigQuery: `storage` (default) or `rest`. See [Choosing the read API](#choosing-the-read-api).

### Authentication

ingestr supports multiple authentication methods for BigQuery:

1. **Explicit credentials** (via `credentials_path` or `credentials_base64` in URI):
   ```plaintext
   bigquery://my-project?credentials_path=/path/to/service-account.json
   ```

2. **Application Default Credentials** (recommended for local development and GCP environments):
   ```plaintext
   bigquery://my-project
   ```
   
   When no credentials are provided in the URI, ingestr will use the Google authentication library which automatically discovers credentials from:
   - The `GOOGLE_APPLICATION_CREDENTIALS` environment variable
   - User credentials set via `gcloud auth application-default login`
   - Service account credentials when running on Google Cloud (Compute Engine, App Engine, Cloud Run, etc.)

The same URI structure can be used both for sources and destinations. Source-only parameters such as `read_api` are ignored when the URI is used as a destination.

### Choosing the read API

By default (`read_api=storage`, or no `read_api`), ingestr reads tables through the [BigQuery Storage Read API](https://cloud.google.com/bigquery/docs/reference/storage), which streams Arrow data in parallel and is the fastest option. Custom queries and any table read the Storage Read API cannot serve run through the BigQuery ADBC driver, which also fetches query results through the Storage Read API.

Set `read_api=rest` to read only through the BigQuery REST API and never touch the Storage Read API:

```plaintext
bigquery://my-project?credentials_path=/path/to/service-account.json&read_api=rest
```

With `read_api=rest`:
- Whole-table reads use `tabledata.list`, which is free and runs no query.
- Filtered reads (`--incremental-key` with `--interval-start`/`--interval-end`, `--extract-partition-by`, `--sql-limit`), views, materialized views, external tables, and custom queries (`query:...`) run as a query job, and ingestr pages through the results with `jobs.getQueryResults`. Query jobs are billed like any other BigQuery query.
- Rows are converted to the same Arrow types the Storage Read API produces, so destination tables come out the same.
- Errors from the REST API are returned as is. ingestr never falls back to the Storage Read API.

Use `read_api=rest` when:
- the credentials lack the `bigquery.readsessions.*` permissions (for example `roles/bigquery.readSessionUser`) that the Storage Read API needs,
- an organization policy or VPC Service Controls perimeter blocks the Storage Read API, or
- you don't want to pay for Storage Read API usage.

The REST API returns rows as JSON, one page at a time, over a single connection. Expect it to be much slower than the Storage Read API on large tables. Keep the default unless one of the reasons above applies.

```bash
ingestr ingest
    --source-uri 'bigquery://my-project?credentials_path=/path/to/service-account.json&read_api=rest' # [!code focus]
    --source-table 'my_dataset.events'
    --dest-uri $DEST_URI
    --dest-table 'raw.events'
```

### Using GCS as a staging area

ingestr can use GCS as a staging area for BigQuery. To do this, you need to set the `--staging-bucket` flag when you are running the command.

```bash
ingestr ingest 
    --source-uri $SOURCE_URI
    --dest-uri $BIGQUERY_URI
    --source-table raw.input 
    --dest-table raw.output
    --staging-bucket "gs://your-bucket-name" # [!code focus]
```


### Partitioning

`--partition-by` accepts a column name or a BigQuery partition expression, the same forms you would write in `PARTITION BY`:

| `--partition-by` | Partitioning |
|---|---|
| `created_at` | Daily (default) |
| `DATE(created_at)` | Daily |
| `TIMESTAMP_TRUNC(created_at, HOUR)` | Hourly; also `DAY`, `MONTH`, `YEAR` |
| `DATETIME_TRUNC(created_at, MONTH)` | Monthly |
| `DATE_TRUNC(created_date, YEAR)` | Yearly |
| `RANGE_BUCKET(customer_id, GENERATE_ARRAY(0, 1000000, 1000))` | Integer ranges of 1000 from 0 to 1,000,000 |

Only the unit of a `*_TRUNC` expression matters; ingestr uses the function that matches the column's type.

```bash
ingestr ingest
    --source-uri $SOURCE_URI
    --dest-uri $BIGQUERY_URI
    --source-table raw.events
    --dest-table raw.events
    --partition-by "TIMESTAMP_TRUNC(created_at, HOUR)" # [!code focus]
```

Changing the partitioning of an existing table is applied by the `replace` strategy (the default) and by `--full-refresh`. Other strategies keep the existing table's partitioning.

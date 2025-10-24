# Socrata

[Socrata](https://dev.socrata.com/) is an open data platform used by governments and organizations to publish and share public datasets. The platform powers thousands of open data portals worldwide, including data.gov and many city, state, and federal government sites.

`ingestr` allows ingesting data from any Socrata-powered open data portal using the [Socrata Open Data API (SODA)](https://dev.socrata.com/docs/endpoints.html).

## URI Format

The URI format for Socrata is as follows:
```
socrata://?domain=<domain>&dataset_id=<dataset_id>&app_token=<app_token>
```

URI Parameters:
* `domain`: The Socrata domain (e.g., `data.seattle.gov`, `data.cityofnewyork.us`)
* `dataset_id`: The dataset identifier (4x4 format like `6udu-fhnu`)
* `app_token`: Socrata app token for API access (required)
* `username` (optional): Username for authentication (required for private datasets)
* `password` (optional): Password for authentication (required for private datasets)

## Setting up Socrata Integration

### Finding Domain and Dataset ID

1. Navigate to any Socrata-powered open data portal
2. Find the dataset you want to ingest
3. The domain is the base URL (e.g., `data.seattle.gov`)
4. The dataset ID is in the URL or API endpoint (e.g., `6udu-fhnu`)

Example: For `https://data.seattle.gov/City-Business/City-of-Seattle-Wage-Data/2khk-5ukd`, the domain is `data.seattle.gov` and the dataset ID is `2khk-5ukd`.

### Generate an App Token

You need to obtain an app token to access the Socrata API.

1. Sign up for a free account at the Socrata portal you're using
2. Navigate to the developer settings or API documentation
3. Generate an app token
4. Use this token in the `app_token` parameter

### Example: Loading a Public Dataset

For this example, we'll load the Seattle City wage data:
* Domain: `data.seattle.gov`
* Dataset ID: `2khk-5ukd`
* App token: `your_app_token_here`

We will run `ingestr` to save this data to a [duckdb](https://duckdb.org/) database called `socrata.db` under the table name `public.wage_data`.

```sh
ingestr ingest \
    --source-uri "socrata://?domain=data.seattle.gov&dataset_id=2khk-5ukd&app_token=your_app_token_here" \
    --source-table "dataset" \
    --dest-uri "duckdb:///socrata.db" \
    --dest-table "public.wage_data"
```

### Example: Incremental Loading

Socrata supports incremental loading using the `:updated_at` system field or any other date/timestamp field in your dataset that gets updated. You must specify the incremental key using the `--incremental-key` flag.

First, run an initial load for a specific time range using `:updated_at` as the incremental key:

```sh
ingestr ingest \
    --source-uri "socrata://?domain=data.seattle.gov&dataset_id=2khk-5ukd&app_token=your_app_token_here" \
    --source-table "dataset" \
    --dest-uri "duckdb:///socrata.db" \
    --dest-table "public.wage_data" \
    --incremental-key ":updated_at" \
    --interval-start "2024-01-01" \
    --interval-end "2024-06-30" \
    --incremental-strategy "merge"
```



Now, we will run `ingestr` again without specifying dates to load only new or updated records:

```sh
ingestr ingest \
    --source-uri "socrata://?domain=data.seattle.gov&dataset_id=2khk-5ukd&app_token=your_app_token_here" \
    --source-table "dataset" \
    --dest-uri "duckdb:///socrata.db" \
    --dest-table "public.wage_data" \
    --incremental-key ":updated_at"
```

This will automatically fetch only records that were created or updated after the last ingestion.

### Example: Loading Private Datasets

For private datasets that require authentication:

```sh
ingestr ingest \
    --source-uri "socrata://?domain=your.domain.com&dataset_id=xxxx-xxxx&app_token=your_token&username=your_username&password=your_password" \
    --source-table "dataset" \
    --dest-uri "duckdb:///socrata.db" \
    --dest-table "public.private_data"
```

## Tables

Socrata source provides a single table called `dataset` that represents the Socrata dataset.

| Name | Merge Key | Inc Key | Inc Strategy | Details |
| --- | --- | --- | --- | --- |
| `dataset` | `:id` | user-defined | replace/merge | Loads all records from the specified Socrata dataset. Uses `replace` by default, or `merge` when `--incremental-key` is specified |





## Troubleshooting

### Rate Limit Errors
If you hit rate limits, register for an app token or reduce the frequency of requests.

### Authentication Errors
For private datasets, ensure username and password are correct and that you have access to the dataset.

### Invalid Dataset ID
Verify the dataset ID is in the correct 4x4 format (e.g., `xxxx-xxxx`) and exists on the specified domain.

### Timeout Errors
For very large datasets, the initial load may take time. Consider using date ranges to break up large loads.

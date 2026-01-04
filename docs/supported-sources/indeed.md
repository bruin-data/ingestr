# Indeed

Indeed is a job search and advertising platform that enables employers to post jobs and manage sponsored job campaigns.

ingestr supports Indeed as a source using the [Indeed Ads API](https://docs.indeed.com/sponsored-jobs-api/).

## URI format

The URI format for Indeed is as follows:

```plaintext
indeed://?client_id=<client_id>&client_secret=<client_secret>&employer_id=<employer_id>
```

URI parameters:

- `client_id` (required): OAuth client ID for Indeed API authentication.
- `client_secret` (required): OAuth client secret for Indeed API authentication.
- `employer_id` (required): The employer ID associated with your Indeed account.

## Setting up an Indeed Integration

To set up Indeed integration, you need to:

1. Create an Indeed Developer account
2. Create an API application in the Indeed Developer Portal
3. Obtain OAuth credentials (client_id, client_secret)
4. Get your employer_id from Indeed

Please follow the [Indeed API documentation](https://docs.indeed.com/api/sponsored-jobs-api/sponsored-jobs-api-reference) for detailed setup instructions.

Once you have your credentials, here's a sample command that will copy data from Indeed into a DuckDB database:

```sh
ingestr ingest \
  --source-uri 'indeed://?client_id=your_client_id&client_secret=your_secret&employer_id=your_employer_id' \
  --source-table 'campaigns' \
  --dest-uri 'duckdb:///indeed.duckdb' \
  --dest-table 'dest.campaigns'
```

## Tables

Indeed source allows ingesting the following resources into separate tables:

| Table | PK | Inc Key | Inc Strategy | Details |
| ----- | -- | ------- | ------------ | ------- |
| `campaigns` | - | - | replace | Retrieves all campaigns for the employer |
| `campaign_details` | - | - | replace | Retrieves detailed information for each campaign |
| `campaign_budget` | - | - | replace | Retrieves budget information for each campaign |
| `campaign_jobs` | - | - | replace | Retrieves all jobs associated with each campaign |
| `campaign_properties` | - | - | replace | Retrieves properties for each campaign |
| `campaign_stats` | - | Date | merge | Retrieves daily statistics for each campaign |
| `account` | - | - | replace | Retrieves account information including job sources |
| `traffic_stats` | - | date | merge | Retrieves daily traffic statistics |

## Incremental Loading

The `campaign_stats` and `traffic_stats` tables support incremental loading using date-based merge strategy. Use `--interval-start` and `--interval-end` to specify the date range:

```sh
ingestr ingest \
  --source-uri 'indeed://?client_id=your_client_id&client_secret=your_secret&employer_id=your_employer_id' \
  --source-table 'campaign_stats' \
  --dest-uri 'duckdb:///indeed.duckdb' \
  --dest-table 'dest.campaign_stats' \
  --interval-start '2024-01-01' \
  --interval-end '2024-01-31'
```

## Examples

### Fetch all campaigns

```sh
ingestr ingest \
  --source-uri 'indeed://?client_id=your_client_id&client_secret=your_secret&employer_id=your_employer_id' \
  --source-table 'campaigns' \
  --dest-uri 'duckdb:///indeed.duckdb' \
  --dest-table 'dest.campaigns'
```

### Fetch campaign statistics for a date range

```sh
ingestr ingest \
  --source-uri 'indeed://?client_id=your_client_id&client_secret=your_secret&employer_id=your_employer_id' \
  --source-table 'campaign_stats' \
  --dest-uri 'duckdb:///indeed.duckdb' \
  --dest-table 'dest.campaign_stats' \
  --interval-start '2024-12-01' \
  --interval-end '2024-12-31'
```

### Fetch traffic statistics

```sh
ingestr ingest \
  --source-uri 'indeed://?client_id=your_client_id&client_secret=your_secret&employer_id=your_employer_id' \
  --source-table 'traffic_stats' \
  --dest-uri 'duckdb:///indeed.duckdb' \
  --dest-table 'dest.traffic_stats' \
  --interval-start '2024-12-01' \
  --interval-end '2024-12-31'
```

### Fetch account information

```sh
ingestr ingest \
  --source-uri 'indeed://?client_id=your_client_id&client_secret=your_secret&employer_id=your_employer_id' \
  --source-table 'account' \
  --dest-uri 'duckdb:///indeed.duckdb' \
  --dest-table 'dest.account'
```

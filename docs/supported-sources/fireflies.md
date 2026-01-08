# Fireflies

[Fireflies.ai](https://fireflies.ai/) is an AI-powered meeting assistant that automatically records, transcribes, and analyzes voice conversations from meetings across various video conferencing platforms.

Ingestr supports Fireflies as a source.

## URI format

The URI format for Fireflies is as follows:

```plaintext
fireflies://?api_key=<api-key-here>
```

URI parameters:

- `api_key`: The API key used for authentication with the Fireflies GraphQL API.

The URI is used to connect to the Fireflies API for extracting data. More details can be found in the [Fireflies API documentation](https://docs.fireflies.ai/getting-started/introduction#advantages-of-using-graphql).

## Setting up a Fireflies Integration

To set up Fireflies integration, you need to obtain an API key:

1. Log in to your [Fireflies account](https://app.fireflies.ai/)
2. Go to **Settings** → **Developer Settings** → **API & Integrations**
3. Generate a new API key

Once you have your API key, here's a sample command that will copy the transcripts from Fireflies into a DuckDB database:

```sh
ingestr ingest \
  --source-uri 'fireflies://?api_key=your-api-key-here' \
  --source-table 'transcripts' \
  --dest-uri duckdb:///fireflies.duckdb \
  --dest-table 'main.transcripts'
```

The result of this command will be a table in the `fireflies.duckdb` database.

## Incremental Loading

Fireflies source supports incremental loading for `analytics` and `transcripts` tables. You can use `--interval-start` and `--interval-end` parameters to specify the time range:

```sh
ingestr ingest \
  --source-uri 'fireflies://?api_key=your-api-key-here' \
  --source-table 'transcripts' \
  --dest-uri duckdb:///fireflies.duckdb \
  --dest-table 'main.transcripts' \
  --interval-start '2024-01-01' \
  --interval-end '2024-12-31'
```

> [!NOTE]
> For `analytics`, the API has a 30-day limit per request. ingestr automatically chunks larger date ranges into 30-day intervals.
>
> [!WARNING]
> The `analytics` table returns **pre-aggregated data** for each chunk (e.g., average duration, total meetings). When querying periods longer than the chunk size, each chunk is stored as a separate row with its own aggregations.

## Analytics Granularity

You can customize the chunk size for analytics by appending a granularity suffix to the table name:

| Table Name | Chunk Size | Use Case |
| ---------- | ---------- | -------- |
| `analytics` | 30 days (default) | Monthly reports |
| `analytics:DAY` | 1 day | Daily metrics |
| `analytics:HOUR` | 1 hour | Detailed hourly analysis |
| `analytics:MONTH` | Month boundaries (respects start/end dates) | Calendar month alignment |

Example with daily granularity:

```sh
ingestr ingest \
  --source-uri 'fireflies://?api_key=your-api-key-here' \
  --source-table 'analytics:DAY' \
  --dest-uri duckdb:///fireflies.duckdb \
  --dest-table 'main.analytics_daily' \
  --interval-start '2024-01-01' \
  --interval-end '2024-01-31'
```

> [!NOTE]
> Smaller granularity means more API requests. Use `analytics:HOUR` only for short date ranges to avoid rate limiting.

## Tables

Fireflies source allows ingesting the following sources into separate tables:

| Table | PK | Inc Key | Inc Strategy | Details |
| ----- | -- | ------- | ------------ | ------- |
| `active_meetings` | - | - | replace | Currently active/ongoing meetings in your Fireflies account. |
| `analytics` | start_time, end_time | end_time | merge | Meeting analytics including duration, speaker stats, and sentiment analysis. |
| `channels` | - | - | replace | Channels (workspaces) configured in your Fireflies account. |
| `users` | - | - | replace | Users in your Fireflies team/organization. |
| `user_groups` | - | - | replace | User groups configured in your organization. |
| `transcripts` | id | date | merge | Meeting transcripts with full conversation details, participants, and metadata. |
| `bites` | - | - | replace | Short audio/video clips (bites) extracted from meetings. |
| `contacts` | - | - | replace | Contacts associated with your Fireflies account. |

Use these as `--source-table` parameter in the `ingestr ingest` command.

> [!TIP]
> For loading meeting transcripts incrementally, use the `transcripts` table with `--interval-start` and `--interval-end` parameters. This is recommended for regular sync jobs to avoid re-fetching all historical data.
>
> [!NOTE]
> The `analytics` table uses `start_time` and `end_time` as a composite primary key, so overlapping date ranges will update existing records instead of creating duplicates.

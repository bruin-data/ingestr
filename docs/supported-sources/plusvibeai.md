# Plus Vibe AI

[Plus Vibe AI](https://plusvibe.ai/) is an email marketing and outreach platform that helps businesses automate their email campaigns, manage leads, and track engagement metrics.

ingestr supports Plus Vibe AI as a source.

## URI format

The URI format for Plus Vibe AI is as follows:

```plaintext
plusvibeai://?api_key=<api-key-here>&workspace_id=<workspace-id-here>
```

URI parameters:

- `api_key`: API key for authentication (get from https://app.plusvibe.ai/v2/settings/api-access/)
- `workspace_id`: Workspace ID to access your data

The URI is used to connect to the Plus Vibe AI API for extracting data.

## Setting up a Plus Vibe AI Integration

To set up a Plus Vibe AI integration, you need to:

1. Log in to your Plus Vibe AI account
2. Navigate to Settings > API Access (https://app.plusvibe.ai/v2/settings/api-access/)
3. Generate an API key
4. Find your workspace ID in your account settings

Once you have your API key and workspace ID, here's a sample command that will copy the data from Plus Vibe AI into a DuckDB database:

```sh
ingestr ingest --source-uri 'plusvibeai://?api_key=your_api_key&workspace_id=your_workspace_id' --source-table 'campaigns' --dest-uri duckdb:///plusvibeai.duckdb --dest-table 'campaigns.data'
```

The result of this command will be a table in the `plusvibeai.duckdb` database with JSON columns for nested objects.

## Tables

Plus Vibe AI source allows ingesting the following sources into separate tables:

| Table | PK | Inc Key | Inc Strategy | Details |
| ----- | -- | ------- | ------------ | ------- |
| `campaigns` | id | modified_at | merge | Contains campaign information including configuration, schedules, sequences, and performance metrics. Nested objects (schedule, sequences) are stored as JSON columns. |
| `leads` | _id | modified_at | merge | Contains lead information including contact details, campaign association, engagement metrics, and professional information. |
| `email_accounts` | _id | timestamp_updated | merge | Contains email account configurations including SMTP/IMAP settings, warmup configurations, and analytics data stored in payload JSON. |
| `emails` | id | timestamp_created | merge | Contains email data including message content, headers, thread information, and recipient details. Uses cursor-based pagination. |
| `blocklist` | _id | created_at | merge | Contains blocklist entries for email addresses or domains that should be excluded from campaigns. |
| `webhooks` | _id | modified_at | merge | Contains webhook configurations for receiving real-time notifications about campaign events and lead interactions. |
| `tags` | _id | modified_at | merge | Contains tag information used for organizing and categorizing campaigns, leads, and other resources. |

Use these as `--source-table` parameter in the `ingestr ingest` command.

## Features

### Incremental Loading

Plus Vibe AI source supports incremental loading based on modification timestamps. Each table uses its respective timestamp field to fetch only updated records since the last sync:

- **Campaigns**: Uses `modified_at` field
- **Leads**: Uses `modified_at` field  
- **Email Accounts**: Uses `timestamp_updated` field
- **Emails**: Uses `timestamp_created` field
- **Blocklist**: Uses `created_at` field
- **Webhooks**: Uses `modified_at` field
- **Tags**: Uses `modified_at` field

### Nested Data Handling

The source preserves nested objects as JSON columns with `max_table_nesting=0` to maintain data structure integrity:

- **Campaigns**: Schedule, sequences, and events are stored as JSON
- **Email Accounts**: All configuration data is stored in the `payload` JSON field
- **Emails**: Headers and address information are stored as JSON

### Rate Limiting

Plus Vibe AI API has a rate limit of 5 requests per second. The source automatically handles rate limiting with exponential backoff and retry logic.

### Error Handling

The source includes comprehensive error handling for:
- Authentication failures (401/403)
- Rate limiting (429) 
- Server errors (5xx) with automatic retries
- Network timeouts and connection issues

> [!TIP]
> For optimal performance, use incremental loading for regular syncs and full loading only for initial data extraction or when you need to capture all historical updates.

> [!NOTE]
> The emails endpoint uses cursor-based pagination with `page_trail` parameter, while other endpoints use standard offset-based pagination.

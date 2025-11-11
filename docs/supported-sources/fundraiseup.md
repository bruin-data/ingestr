# FundraiseUp

[FundraiseUp](https://fundraiseup.com/) is a modern donation platform that helps non-profits increase their online fundraising revenue.

ingestr supports FundraiseUp as a source.

## URI format

The URI format for FundraiseUp is:

```
fundraiseup://?api_key=<api_key>
```

URI parameters:
- `api_key`: Your FundraiseUp API key (required).

## Example usage

Assuming your API key is `your_api_key`, you can ingest donations into DuckDB using:

```bash
ingestr ingest \
    --source-uri 'fundraiseup://?api_key=your_api_key' \
    --source-table 'donations' \
    --dest-uri duckdb:///fundraiseup.duckdb \
    --dest-table 'main.donations'
```

## Tables

The FundraiseUp source supports the following tables:

| Table           | PK | Inc Key | Inc Strategy | Details                                                                                                                                        |
| --------------- | ----------- | --------------- | ------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------- |
| `donations`       | id | - | replace               | All donation records including amounts, supporters, and payment details |
| `donations:incremental`       | id | - | merge               | All donation records including amounts, supporters, and payment details. Loads records incrementally. |
| `events`       | id | - | replace               | Audit log events for tracking changes and activities |
| `fundraisers`       | id | - | replace               | Fundraiser campaigns (requires appropriate API permissions) |
| `recurring_plans`       | id | - | replace               | Recurring donation plans and subscription details |
| `supporters`       | id | - | replace               | Donor/supporter information including contact details |

Use one of these as the `--source-table` parameter in the `ingestr ingest` command.

## Notes

- **Authentication**: The FundraiseUp API uses Bearer token authentication. Make sure your API key has the necessary permissions for the resources you want to access.
- **Date Filtering**: The API does not support date filtering for any of the endpoints.
- **Permissions**: The `fundraisers` endpoint may return a 403 Forbidden error if your API key doesn't have the required permissions. Contact FundraiseUp support to enable access if needed.

## Example: Ingesting all available data

To ingest all available data from FundraiseUp:

```bash
# Ingest donations
ingestr ingest \
    --source-uri 'fundraiseup://?api_key=your_api_key' \
    --source-table 'donations' \
    --dest-uri duckdb:///fundraiseup.db \
    --dest-table 'main.donations'

# Ingest events
ingestr ingest \
    --source-uri 'fundraiseup://?api_key=your_api_key' \
    --source-table 'events' \
    --dest-uri duckdb:///fundraiseup.db \
    --dest-table 'main.events'

# Ingest recurring plans
ingestr ingest \
    --source-uri 'fundraiseup://?api_key=your_api_key' \
    --source-table 'recurring_plans' \
    --dest-uri duckdb:///fundraiseup.db \
    --dest-table 'main.recurring_plans'

# Ingest supporters
ingestr ingest \
    --source-uri 'fundraiseup://?api_key=your_api_key' \
    --source-table 'supporters' \
    --dest-uri duckdb:///fundraiseup.db \
    --dest-table 'main.supporters'
```

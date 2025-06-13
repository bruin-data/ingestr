# Trustpilot

[Trustpilot](https://www.trustpilot.com/) provides a platform for collecting and
sharing customer reviews.

ingestr supports Trustpilot as a source.

## URI format

The URI format for Trustpilot is:

```
trustpilot://<business_unit_id>?api_key=<api_key>
```

URI parameters:
- `api_key`: Your Trustpilot API key.
- `business_unit_id`: Identifier of the business unit whose reviews you want to fetch.

## Example usage

Assuming your `business_unit_id` is `123` and your API key is `key_abc`, you can ingest reviews into DuckDB using:

```bash
ingestr ingest --source-uri 'trustpilot://123?api_key=key_abc' --source-table 'reviews' --dest-uri duckdb:///trustpilot.duckdb --dest-table 'dest.reviews'
```

## Tables

Currently the Trustpilot source exposes the following table:

| Name    | Description                                 |
| ------- | ------------------------------------------- |
| reviews | Customer reviews for the specified business |


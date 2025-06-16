# Bing Ads

[Microsoft Advertising API](https://learn.microsoft.com/advertising/index?view=bingads-13) (formerly Bing Ads) allows managing and reporting on advertising campaigns.

ingestr supports Bing Ads as a source.

## URI format

```plaintext
bingads://<account_id>?customer_id=<customer_id>&client_id=<client_id>&client_secret=<client_secret>&refresh_token=<refresh_token>&developer_token=<developer_token>&environment=<environment>
```

URI parameters:

- `account_id`: The numeric account id used as the host portion of the URI.
- `customer_id`: The customer id that owns the account.
- `client_id`: OAuth application client id.
- `client_secret`: OAuth application client secret.
- `refresh_token`: OAuth refresh token.
- `developer_token`: Bing Ads developer token.
- `environment`: (Optional) `production` or `sandbox`. Defaults to `production`.

To obtain the required credentials see the [getting started guide](https://learn.microsoft.com/advertising/guides/get-started?view=bingads-13).

## Example

```sh
ingestr ingest \
  --source-uri 'bingads://123456?customer_id=654321&client_id=my-client&client_secret=my-secret&refresh_token=my-refresh&developer_token=my-dev-token' \
  --source-table 'campaign_performance' \
  --dest-uri duckdb:///bingads.duckdb \
  --dest-table 'bingads.campaigns'
```

## Tables

- `campaign_performance`: Daily campaign performance metrics.

# QuickBooks

[QuickBooks](https://quickbooks.intuit.com/) is accounting software used by many businesses to manage finances.

ingestr supports QuickBooks as a source.

## URI format

```plaintext
quickbooks://<company_id>?client_id=<client_id>&client_secret=<client_secret>&refresh_token=<refresh_token>&access_token=<access_token>&environment=<environment>&minor_version=<minor_version>
```

URI parameters:
- `company_id`: The QuickBooks company (realm) id.
- `client_id`: OAuth client id from your Intuit application.
- `client_secret`: OAuth client secret.
- `refresh_token`: OAuth refresh token used to obtain access tokens.
- `access_token`: Optional OAuth access token. If omitted it will be refreshed automatically.
- `environment`: Optional environment name, either `production` or `sandbox`. Defaults to `production`.
- `minor_version`: Optional API minor version.

## Setting up a QuickBooks integration

Follow Intuit's [OAuth setup guide](https://developer.intuit.com/app/developer/qbo/docs/develop/authentication-and-authorization) to create an app and generate your credentials.

Once you have the credentials, you can ingest data. For example, to copy customers into DuckDB:

```sh
ingestr ingest --source-uri 'quickbooks://1234567890?client_id=cid&client_secret=csecret&refresh_token=rtoken' --source-table 'customers' --dest-uri duckdb:///quickbooks.duckdb --dest-table 'dest.customers'
```

## Tables

QuickBooks source allows ingesting the following tables:

- `customers`: List of customers.
- `invoices`: Sales invoices.
- `accounts`: Chart of accounts.
- `vendors`: Vendor records.
- `payments`: Payments recorded in QuickBooks.

Use these as the `--source-table` parameter in the `ingestr ingest` command.

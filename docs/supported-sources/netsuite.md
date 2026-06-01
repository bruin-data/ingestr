# NetSuite

[NetSuite](https://www.netsuite.com/) is a cloud ERP platform from Oracle.

ingestr supports NetSuite as a source through SuiteTalk REST Web Services and SuiteQL.

## URI Format

Use an existing OAuth 2.0 access token:

```text
netsuite://?account_id=<account_id>&access_token=<access_token>
```

Or use OAuth 2.0 client credentials with a certificate mapping:

```text
netsuite://?account_id=<account_id>&client_id=<client_id>&certificate_id=<certificate_id>&private_key_path=/path/to/private-key.pem
```

URI parameters:

- `account_id`: NetSuite account ID used in the account-specific SuiteTalk REST domain.
- `access_token`: OAuth 2.0 bearer token for REST Web Services.
- `client_id`: OAuth 2.0 integration client ID for machine-to-machine auth.
- `certificate_id`: Certificate ID from OAuth 2.0 Client Credentials (M2M) Setup. `kid` is accepted as an alias.
- `private_key_path`: Path to the private key that matches the certificate uploaded to NetSuite.
- `private_key`: Inline PEM private key. Newlines can be encoded as `\n`.
- `scope`: OAuth scopes for the client credentials assertion. Defaults to `rest_webservices`.
- `algorithm`: JWT signing algorithm. Defaults to `PS256`; supported values are `PS256`, `PS384`, `PS512`, `ES256`, `ES384`, and `ES512`.
- `base_url`: Optional override for the SuiteTalk REST base URL.

## Examples

Load the `customer` SuiteQL table into DuckDB:

```bash
ingestr ingest \
  --source-uri "netsuite://?account_id=${NETSUITE_ACCOUNT_ID}&access_token=${NETSUITE_ACCESS_TOKEN}" \
  --source-table "customer" \
  --dest-uri "duckdb:///netsuite.duckdb" \
  --dest-table "main.netsuite_customers"
```

Use a custom SuiteQL query:

```bash
ingestr ingest \
  --source-uri "netsuite://?account_id=${NETSUITE_ACCOUNT_ID}&client_id=${NETSUITE_CLIENT_ID}&certificate_id=${NETSUITE_CERTIFICATE_ID}&private_key_path=${NETSUITE_PRIVATE_KEY_PATH}" \
  --source-table "query:SELECT id, entityid, email FROM customer" \
  --dest-uri "duckdb:///netsuite.duckdb" \
  --dest-table "main.netsuite_customer_emails"
```

Use interval filtering by passing an incremental key:

```bash
ingestr ingest \
  --source-uri "netsuite://?account_id=${NETSUITE_ACCOUNT_ID}&access_token=${NETSUITE_ACCESS_TOKEN}" \
  --source-table "transaction" \
  --incremental-key "lastmodifieddate" \
  --interval-start "2026-01-01T00:00:00Z" \
  --interval-end "2026-02-01T00:00:00Z" \
  --dest-uri "duckdb:///netsuite.duckdb" \
  --dest-table "main.netsuite_transactions"
```

For plain table names, ingestr runs `SELECT * FROM <source-table>` through SuiteQL. For joins, selected columns, built-in functions, or aliases, use the `query:` source-table form.

NetSuite SuiteQL responses do not expose a static schema through this connector, so ingestr infers the schema from extracted rows.

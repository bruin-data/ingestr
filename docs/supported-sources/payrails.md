# Payrails

[Payrails](https://www.payrails.com/) is a payment operations and orchestration platform that lets enterprises connect and manage multiple payment providers through a single integration.

ingestr supports Payrails as a source.

## URI format

The URI format for Payrails is as follows:

```plaintext
payrails://?client_id=<client-id>&client_secret=<client-secret>&environment=<environment>&cert_path=<path-to-cert>&key_path=<path-to-key>
```

URI parameters:

- `client_id`: the ID of your Payrails API credential
- `client_secret`: the secret of your Payrails API credential
- `environment` (optional): `production` (default, `https://api.payrails.io`) or `sandbox` (`https://api.staging.payrails.io`)
- `base_url` (optional): the full API base URL. Overrides `environment`; use it if Payrails gives you an account-specific host.
- `cert_path`: path to your mTLS client certificate (`.pem`)
- `key_path`: path to your mTLS client private key (`.key`)

You can create API credentials in the Payrails Portal under **Settings > API Credentials** (admin or developer role required), and set up mTLS following the [Payrails mTLS documentation](https://docs.payrails.com/docs/mtls-configuration-1).

## Tables

Payrails source allows ingesting the following into separate tables:

| Table | Primary key | Incremental key | Strategy | Details |
|-------|-------------|-----------------|----------|---------|
| `payments` | `id` | `createdAt` | merge | Payments with their status, amount, references, and transaction metadata. |
| `instruments` | `id` | `createdAt` | merge | Stored payment instruments (e.g. tokenized cards) with their status and metadata. |
| `executions` | `id` | `updatedAt` | merge | Workflow executions with their status and references. Requires a workflow code (see below). |

Use these as the `--source-table` parameter in the `ingestr ingest` command.

## Setting up a Payrails integration

Here's a sample command that copies payments from Payrails into a DuckDB database:

```sh
ingestr ingest \
  --source-uri "payrails://?client_id=your_client_id&client_secret=your_client_secret&environment=sandbox&cert_path=/path/to/client.pem&key_path=/path/to/client.key" \
  --source-table "payments" \
  --dest-uri "duckdb:///payrails.duckdb" \
  --dest-table "dest.payments"
```

### Workflow executions
You must specify which workflow(s) to pull executions for by appending the workflow code(s) to the table name after a colon:

```sh
ingestr ingest \
  --source-uri "payrails://?client_id=your_client_id&client_secret=your_client_secret&cert_path=/path/to/client.pem&key_path=/path/to/client.key" \
  --source-table "executions:payment-acceptance" \
  --dest-uri "duckdb:///payrails.duckdb" \
  --dest-table "dest.executions"
```

You can pass multiple workflow codes as a comma-separated list (e.g. `executions:payment-acceptance,payout`). Each row includes a `workflow_code` column identifying the workflow it belongs to.

## Incremental loading

The `payments` and `instruments` tables support incremental loading by date range (filtered server-side on `createdAt`); `executions` is filtered on `updatedAt`. Use `--interval-start` and `--interval-end` to bound the records:

```sh
ingestr ingest \
  --source-uri "payrails://?client_id=your_client_id&client_secret=your_client_secret&cert_path=/path/to/client.pem&key_path=/path/to/client.key" \
  --source-table "payments" \
  --dest-uri "duckdb:///payrails.duckdb" \
  --dest-table "dest.payments" \
  --interval-start "2024-01-01" \
  --interval-end "2024-01-31"
```

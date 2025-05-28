# Solidgate

[Solidgate](https://solidgate.com//) is a one-stop payment processing platform that ensures seamless online purchases and streamlined payment infrastructure.

ingestr supports Solidgate as a source.

## URI format

The URI format for Solidgate is as follows:

```plaintext
solidgate://?public_key=<your-public-key>&secret_key=<your-secret-key>
```

URI parameters:

- `public_key`: The public API key used to identify the account.

- `secret_key`: The secret API key used to authenticate requests to the Solidgate API.


## Setting up a Solidgate Integration

Solidgate requires a few steps to set up an integration. Please follow the [guide](https://docs.solidgate.com/payments/integrate/access-to-api/#retrieve-your-credentials).

Once you complete the guide, you should have `public_key` and `secret_key`, here’s a sample command that ingests data from Solidgate into a DuckDB database:

```sh
ingestr ingestr ingest \
--source-uri "solidgate://?public_key=api_pk_test&secret_key=api_sk_test" \
--source-table "apm-orders" \
--dest-uri "duckdb:///solidgate.db" \
--dest-table "dest.apmorders"
```

The result of this command will be a table in the `Solidgate.duckdb` database with JSON columns.

## Tables

Solidgate source allows ingesting the following sources into separate tables:
- `subscriptions`: Provides a comprehensive view of customer subscriptions, including subscription IDs, statuses, and key timestamps such as creation, update, and expiration dates.
- `apm-orders`: Provides essential information for anti-fraud purposes, including order IDs, transaction statuses, amounts, currencies, and payment methods, along with crucial customer details such as email addresses.
- `card-orders`: Provides detailed information on orders processed via card payments, including transaction data, payment status, and customer details.

Use these as `--source-table` parameter in the `ingestr ingest` command.


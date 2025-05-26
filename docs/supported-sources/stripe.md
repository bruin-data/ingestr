# Stripe

[Stripe](https://www.stripe.com/) is a technology company that builds economic infrastructure for the internet, providing payment processing software and APIs for e-commerce websites and mobile applications.

ingestr supports Stripe as a source.

## URI format

The URI format for Stripe is as follows:

```plaintext
stripe://?api_key=<api-key-here>
```

URI parameters:

- `api_key`: the API key used for authentication with the Stripe API

The URI is used to connect to the Stripe API for extracting data. More details on setting up Stripe integrations can be found [here](https://stripe.com/docs/api).

## Setting up a Stripe Integration

Stripe requires a few steps to set up an integration, please follow the guide dltHub [has built here](https://dlthub.com/docs/dlt-ecosystem/verified-sources/stripe#setup-guide).

Once you complete the guide, you should have an API key. Let's say your API key is `sk_test_12345`, here's a sample command that will copy the data from Stripe into a DuckDB database:

```sh
ingestr ingest --source-uri 'stripe://?api_key=sk_test_12345' --source-table 'charges' --dest-uri duckdb:///stripe.duckdb --dest-table 'dest.charges'
```

The result of this command will be a table in the `stripe.duckdb` database with JSON columns.

## Tables

Stripe source allows ingesting the following sources into separate tables:

- `subscription`: Represents a customer's subscription to a recurring service, detailing billing cycles, plans, and status.
- `account`: Contains information about a Stripe account, including balances, payouts, and account settings.
- `coupon`: Stores data about discount codes or coupons that can be applied to invoices, subscriptions, or other charges.
- `customer`: Holds information about customers, such as billing details, payment methods, and associated transactions.
- `product`: Represents products that can be sold or subscribed to, including metadata and pricing information.
- `price`: Contains pricing information for products, including currency, amount, and billing intervals.
- `balancetransaction`: Records transactions that affect the Stripe account balance, such as charges, refunds, and payouts.
- `invoice`: Represents invoices sent to customers, detailing line items, amounts, and payment status.
- `event`: Logs all events in the Stripe account, including customer actions, account updates, and system-generated events.
- `charge`: Returns a list of charges.

Use these as `--source-table` parameter in the `ingestr ingest` command.

> [!WARNING]
> Stripe does not support incremental loading for many endpoints in its APIs, which means ingestr will load endpoints incrementally if they support it, and do a full-refresh if not.

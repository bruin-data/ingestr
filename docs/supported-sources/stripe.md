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

### Regular Endpoints

- `account`: Contains information about a Stripe account, including balances, payouts, and account settings.
- `apple_pay_domain`: Represents Apple Pay domains registered with Stripe for processing Apple Pay payments.
- `application_fee`: Records fees collected by platforms on payments processed through connected accounts.
- `checkout_session`: Contains data about Checkout sessions created for payment processing workflows.
- `coupon`: Stores data about discount codes or coupons that can be applied to invoices, subscriptions, or other charges.
- `customer`: Holds information about customers, such as billing details, payment methods, and associated transactions.
- `dispute`: Records payment disputes and chargebacks filed by customers or banks.
- `payment_intent`: Represents payment intents tracking the lifecycle of payments from creation to completion.
- `payment_link`: Contains information about payment links created for collecting payments.
- `payment_method`: Stores payment method information such as cards, bank accounts, and other payment instruments.
- `payment_method_domain`: Represents domains verified for payment method collection.
- `payout`: Records payouts made from Stripe accounts to bank accounts or debit cards.
- `plan`: Contains subscription plan information including pricing and billing intervals.
- `price`: Contains pricing information for products, including currency, amount, and billing intervals.
- `product`: Represents products that can be sold or subscribed to, including metadata and pricing information.
- `promotion_code`: Stores data about promotion codes that customers can use to apply coupons.
- `quote`: Contains quote information for customers, including line items and pricing.
- `refund`: Records refunds issued for charges, including partial and full refunds.
- `review`: Contains payment review information for payments flagged by Stripe Radar.
- `setup_attempt`: Records attempts to set up payment methods for future payments.
- `setup_intent`: Represents setup intents for collecting payment method information.
- `shipping_rate`: Contains shipping rate information for orders and invoices.
- `subscription`: Represents a customer's subscription to a recurring service, detailing billing cycles, plans, and status.
- `subscription_item`: Contains individual items within a subscription, including quantities and pricing.
- `subscription_schedule`: Represents scheduled changes to subscriptions over time.
- `tax_code`: Contains tax code information for products and services.
- `tax_id`: Stores tax ID information for customers and accounts.
- `tax_rate`: Contains tax rate information applied to invoices and subscriptions.
- `top_up`: Records top-ups made to Stripe accounts.
- `transfer`: Records transfers between Stripe accounts.
- `webhook_endpoint`: Contains webhook endpoint configurations for receiving event notifications.

### Incremental Endpoints

The following endpoints support incremental loading, meaning only new or updated records will be fetched:

- `application_fee`: Records fees collected by platforms (incremental).
- `balance_transaction`: Records transactions that affect the Stripe account balance, such as charges, refunds, and payouts.
- `charge`: Returns a list of charges.
- `credit_note`: Contains credit note information for refunds and adjustments.
- `event`: Logs all events in the Stripe account, including customer actions, account updates, and system-generated events.
- `invoice`: Represents invoices sent to customers, detailing line items, amounts, and payment status.
- `invoice_item`: Contains individual line items that can be added to invoices.
- `invoice_line_item`: Represents line items within invoices.
- `setup_attempt`: Records attempts to set up payment methods (also available as incremental).

Use these as `--source-table` parameter in the `ingestr ingest` command.

> [!WARNING]
> Stripe does not support incremental loading for many endpoints in its APIs, which means ingestr will load endpoints incrementally if they support it, and do a full-refresh if not.

> [!NOTE]
> For backward compatibility, non-underscored versions of table names (e.g., `checkoutsession`, `paymentintent`, `subscriptionitem`) are still supported but will be deprecated in future versions. Please use the underscored versions (e.g., `checkout_session`, `payment_intent`, `subscription_item`) for new integrations.

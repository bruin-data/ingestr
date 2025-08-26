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

## Table Name Structure

Stripe source supports different loading modes that can be specified using the table name structure:

- `<endpoint>` - Standard async loading (default)
- `<endpoint>:sync` - Full loading with synchronous processing
- `<endpoint>:sync:incremental` - Incremental loading mode with synchronous processing

### Loading Modes and Trade-offs

#### Standard Async Loading (Default)
**Format**: `<endpoint>` (e.g., `charges`, `subscriptions`)

- **Use case**: Full data loading from all time periods
- **Performance**: Loads data in parallel using async processing
- **Data completeness**: Captures all historical data and updates
- **Speed**: Slower due to comprehensive data retrieval
- **Best for**: You want to have all updated data in your database

**Example**:
```sh
ingestr ingest --source-uri 'stripe://?api_key=sk_test_12345' --source-table 'subscriptions' --dest-uri duckdb:///stripe.duckdb --dest-table 'dest.subscriptions'
```

#### Sync Loading
**Format**: `<endpoint>:sync` (e.g., `charges:sync`, `subscriptions:sync`)

- **Use case**: Full data loading from all time periods
- **Performance**: Loads data in parallel using sync processing
- **Data completeness**: Captures all historical data and updates
- **Speed**: Slower due to comprehensive data retrieval, faster if you have less data

#### Incremental Loading
**Format**: `<endpoint>:sync:incremental` (e.g., `charges:sync:incremental`, `events:sync:incremental`)

- **Use case**: Loading data within specific time windows
- **Performance**: Fast, processes only data within the specified interval
- **Data completeness**: Limited to the specified time window, does not track updates from past dates
- **Speed**: Faster due to filtered data retrieval
- **Processing**: Runs in synchronous mode only
- **Best for**: Quick loads, you don't care about the updates to past data

**Example**:
```sh
ingestr ingest --source-uri 'stripe://?api_key=sk_test_12345' --source-table 'charges:sync:incremental' --dest-uri duckdb:///stripe.duckdb --dest-table 'dest.charges' --interval-start '2024-01-01' --interval-end '2024-01-31'
```

### Choosing the Right Approach

| Approach | Speed | Data Completeness | Use Case |
|----------|--------|------------------|----------|
| **Standard Async** | Faster for larger data, slower for smaller data | Complete historical data | Initial loads, full historical analysis |
| **Sync** | Slow for larger data, faster for smaller data | Complete historical data | Initial loads, full historical analysis |
| **Incremental** | Fastest | Time-window specific | Regular updates, recent data analysis |

## Tables

Stripe source allows ingesting the following sources into separate tables:

### All Endpoints

All endpoints support the standard async loading mode. The following endpoints are available:

| Table | PK | Inc Key | Inc Strategy | Details |
| ----- | -- | ------- | ------------ | ------- |
| `account` | id | created | merge | Contains information about a Stripe account, including balances, payouts, and account settings. |
| `apple_pay_domain` | id | created | merge | Represents Apple Pay domains registered with Stripe for processing Apple Pay payments. |
| `application_fee` | id | created | merge | Records fees collected by platforms on payments processed through connected accounts. |
| `balance_transaction` | id | created | merge | Records transactions that affect the Stripe account balance, such as charges, refunds, and payouts. |
| `charge` | id | created | merge | Returns a list of charges. |
| `checkout_session` | id | created | merge | Contains data about Checkout sessions created for payment processing workflows. |
| `coupon` | id | created | merge | Stores data about discount codes or coupons that can be applied to invoices, subscriptions, or other charges. |
| `credit_note` | id | created | merge | Contains credit note information for refunds and adjustments. |
| `customer` | id | created | merge | Holds information about customers, such as billing details, payment methods, and associated transactions. |
| `dispute` | id | created | merge | Records payment disputes and chargebacks filed by customers or banks. |
| `event` | id | created | merge | Logs all events in the Stripe account, including customer actions, account updates, and system-generated events. |
| `invoice` | id | created | merge | Represents invoices sent to customers, detailing line items, amounts, and payment status. |
| `invoice_item` | id | created | merge | Contains individual line items that can be added to invoices. |
| `invoice_line_item` | id | created | merge | Represents line items within invoices. |
| `payment_intent` | id | created | merge | Represents payment intents tracking the lifecycle of payments from creation to completion. |
| `payment_link` | id | created | merge | Contains information about payment links created for collecting payments. |
| `payment_method` | id | created | merge | Stores payment method information such as cards, bank accounts, and other payment instruments. |
| `payment_method_domain` | id | created | merge | Represents domains verified for payment method collection. |
| `payout` | id | created | merge | Records payouts made from Stripe accounts to bank accounts or debit cards. |
| `plan` | id | created | merge | Contains subscription plan information including pricing and billing intervals. |
| `price` | id | created | merge | Contains pricing information for products, including currency, amount, and billing intervals. |
| `product` | id | created | merge | Represents products that can be sold or subscribed to, including metadata and pricing information. |
| `promotion_code` | id | created | merge | Stores data about promotion codes that customers can use to apply coupons. |
| `quote` | id | created | merge | Contains quote information for customers, including line items and pricing. |
| `refund` | id | created | merge | Records refunds issued for charges, including partial and full refunds. |
| `review` | id | created | merge | Contains payment review information for payments flagged by Stripe Radar. |
| `setup_attempt` | id | created | merge | Records attempts to set up payment methods for future payments. |
| `setup_intent` | id | created | merge | Represents setup intents for collecting payment method information. |
| `shipping_rate` | id | created | merge | Contains shipping rate information for orders and invoices. |
| `subscription` | id | created | merge | Represents a customer's subscription to a recurring service, detailing billing cycles, plans, and status. |
| `subscription_item` | id | created | merge | Contains individual items within a subscription, including quantities and pricing. |
| `subscription_schedule` | id | created | merge | Represents scheduled changes to subscriptions over time. |
| `tax_code` | id | created | merge | Contains tax code information for products and services. |
| `tax_id` | id | created | merge | Stores tax ID information for customers and accounts. |
| `tax_rate` | id | created | merge | Contains tax rate information applied to invoices and subscriptions. |
| `top_up` | id | created | merge | Records top-ups made to Stripe accounts. |
| `transfer` | id | created | merge | Records transfers between Stripe accounts. |
| `webhook_endpoint` | id | created | merge | Contains webhook endpoint configurations for receiving event notifications. |

Use these as `--source-table` parameter in the `ingestr ingest` command.

> [!TIP]
> For time-sensitive data analysis or regular updates, use incremental loading (`:incremental`) with `--interval-start` and `--interval-end` parameters for faster processing. For comprehensive historical analysis, use standard async loading without any suffix.

> [!WARNING]
> Incremental loading filters data based on the specified time window and does not track updates to records created outside that window. Use standard async loading if you need to capture all historical updates.

> [!NOTE]
> For backward compatibility, non-underscored versions of table names (e.g., `checkoutsession`, `paymentintent`, `subscriptionitem`) are still supported but will be deprecated in future versions. Please use the underscored versions (e.g., `checkout_session`, `payment_intent`, `subscription_item`) for new integrations.

# Square

[Square](https://squareup.com/) is a payments and commerce platform for businesses.

ingestr supports Square as a source.

## URI format

The URI format for Square is as follows:

```plaintext
square://?access_token=<access-token>
```

URI parameters:

- `access_token` (required): A Square access token for your application or seller account.
- `environment`: Either `production` (default) or `sandbox`. Use `sandbox` to read from Square's developer sandbox.

## Setting up a Square Integration

To get an access token:

1. Sign in to the [Square Developer Dashboard](https://developer.squareup.com/apps).
2. Open an existing application or create a new one.
3. Copy the **Access token** from the **Credentials** page. Use the **Sandbox** token together with `environment=sandbox` for testing, or the **Production** token for live data.

Once you have your access token, here's a sample command that will copy data from Square into a DuckDB database:

```sh
ingestr ingest \
  --source-uri 'square://?access_token=EAAAxxxxxx' \
  --source-table 'payments' \
  --dest-uri duckdb:///square.duckdb \
  --dest-table 'square.payments'
```

The result of this command will be a table in the `square.duckdb` database.

## Tables

Square source allows ingesting the following sources into separate tables:

| Table | PK | Inc Key | Inc Strategy | Details |
| ----- | -- | ------- | ------------ | ------- |
| `payments` | `id` | `updated_at` | merge | Payments taken by the account. |
| `refunds` | `id` | `updated_at` | merge | Refunds processed against payments. |
| `orders` | `id` | `updated_at` | merge | Orders across all of the account's locations. |
| `customers` | `id` | `updated_at` | merge | Customer profiles. |
| `catalog_objects` | `id` | `updated_at` | merge | All catalog objects (items, variations, categories, taxes, discounts, modifier lists, images, and more). |
| `locations` | `id` | - | replace | The account's locations. |
| `team_members` | `id` | `updated_at` | merge | Team members (staff). Soft-deletes appear as `status="INACTIVE"`. |
| `team_member_wages` | `id` | - | replace | Hourly wage settings per team member. |
| `shifts` | `id` | - | replace | Worked shifts (Labor API). |
| `inventory` | `catalog_object_id`, `location_id`, `state` | `calculated_at` | merge | Inventory counts per catalog object, location, and state. |
| `bank_accounts` | `id` | - | replace | Linked bank accounts. |
| `cash_drawers` | `id` | - | replace | Cash drawer shifts across all locations. |
| `loyalty` | `id` | - | replace | Loyalty accounts. |

Use one of these as the `--source-table` parameter in the `ingestr ingest` command.

## Examples

Ingest orders updated within a given interval:

```sh
ingestr ingest \
  --source-uri 'square://?access_token=EAAAxxxxxx' \
  --source-table 'orders' \
  --dest-uri duckdb:///square.duckdb \
  --dest-table 'square.orders' \
  --interval-start 2024-01-01
```

Read from the Square sandbox:

```sh
ingestr ingest \
  --source-uri 'square://?access_token=EAAAxxxxxx&environment=sandbox' \
  --source-table 'catalog_objects' \
  --dest-uri duckdb:///square.duckdb \
  --dest-table 'square.catalog_objects'
```

# Hostaway

[Hostaway](https://www.hostaway.com/) is a property management system (PMS) designed for vacation rental managers and hosts. It provides tools for managing listings, reservations, channels, and guest communications across multiple booking platforms.

ingestr supports Hostaway as a source.

## URI format

The URI format for Hostaway is as follows:

```plaintext
hostaway://?api_key=<api-key-here>
```

URI parameters:

- `api_key`: the API access token used for authentication with the Hostaway API

The URI is used to connect to the Hostaway API for extracting data. More details on the Hostaway API can be found in the [official API documentation](https://api-docs.hostaway.com/).

## Setting up a Hostaway Integration

Hostaway uses OAuth 2.0 client credentials for authentication. Follow these steps to obtain an API access token:

### 1. Get Your Credentials

First, you need your Hostaway account credentials:
- `client_id`: Your Hostaway account ID
- `client_secret`: Your API client secret (available in Hostaway settings)

### 2. Generate an Access Token

Use the following curl command to generate an access token:

```bash
curl -X POST https://api.hostaway.com/v1/accessTokens \
  -H 'Cache-control: no-cache' \
  -H 'Content-type: application/x-www-form-urlencoded' \
  -d 'grant_type=client_credentials&client_id=YOUR_ACCOUNT_ID&client_secret=YOUR_CLIENT_SECRET&scope=general'
```

The response will contain an access token (JWT) that you'll use for authentication.

### 3. Use the Access Token

Once you have your access token, you can use it with ingestr. Here's a sample command that will copy listings data from Hostaway into a DuckDB database:

```sh
ingestr ingest \
  --source-uri 'hostaway://?api_key=YOUR_ACCESS_TOKEN' \
  --source-table 'listings' \
  --dest-uri 'duckdb:///hostaway.duckdb' \
  --dest-table 'main.listings' \
  --interval-start '2020-01-01' \
  --interval-end '2025-12-31'
```

### 4. Revoking Access Tokens

To revoke an access token when it's no longer needed:

```bash
curl -X DELETE 'https://api.hostaway.com/v1/accessTokens?token=YOUR_ACCESS_TOKEN' \
  -H 'Content-type: application/x-www-form-urlencoded'
```

## Tables

Hostaway source allows ingesting the following resources into separate tables:

| Table | PK | Inc Key | Inc Strategy | Description |
| ----- | -- | ------- | ------------ | ----------- |
| `listings` | id | latestActivityOn | merge | Property listings managed in Hostaway |
| `listing_fee_settings` | id | updatedOn | merge | Fee settings configured for each listing |
| `listing_pricing_settings` | - | - | replace | Pricing rules and settings for listings |
| `listing_agreements` | - | - | replace | Rental agreements associated with listings |
| `listing_calendars` | - | - | replace | Calendar availability data for each listing. Uses parallelization for performance |
| `cancellation_policies` | - | - | replace | General cancellation policies |
| `cancellation_policies_airbnb` | - | - | replace | Airbnb-specific cancellation policies |
| `cancellation_policies_marriott` | - | - | replace | Marriott-specific cancellation policies |
| `cancellation_policies_vrbo` | - | - | replace | VRBO-specific cancellation policies |
| `reservations` | - | - | replace | Booking reservations across all channels |
| `finance_fields` | - | - | replace | Financial data for each reservation. Uses parallelization for performance |
| `reservation_payment_methods` | - | - | replace | Available payment methods for reservations |
| `reservation_rental_agreements` | - | - | replace | Rental agreements for specific reservations. Uses parallelization for performance |
| `conversations` | - | - | replace | Guest communication threads |
| `message_templates` | - | - | replace | Pre-configured message templates |
| `bed_types` | - | - | replace | Available bed type configurations |
| `property_types` | - | - | replace | Property type classifications |
| `countries` | - | - | replace | Supported countries and their codes |
| `account_tax_settings` | - | - | replace | Tax configuration for the account |
| `user_groups` | - | - | replace | User groups and permissions |
| `guest_payment_charges` | - | - | replace | Guest payment transaction records |
| `coupons` | - | - | replace | Discount coupons and promotional codes |
| `webhook_reservations` | - | - | replace | Webhook configurations for reservation events |
| `tasks` | - | - | replace | Tasks and to-dos within the system |

Use these table names as the `--source-table` parameter in the `ingestr ingest` command.

## Notes

- The `finance_fields`, `reservation_rental_agreements`, and `listing_calendars` tables use parallelization for improved performance when fetching nested resource data
- Some endpoints like `listings` and `reservations` support incremental loading using `--interval-start` and `--interval-end` parameters
- Access tokens are JWTs with configurable expiration times - manage them securely and rotate them as needed

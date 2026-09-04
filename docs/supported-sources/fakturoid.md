# Fakturoid

[Fakturoid](https://www.fakturoid.cz/) is a Czech invoicing and accounting service for freelancers and small businesses.

ingestr supports Fakturoid as a source through the [Fakturoid API v3](https://www.fakturoid.cz/api/v3).

## URI format

```plaintext
fakturoid://?client_id=<client_id>&client_secret=<client_secret>&slug=<slug>&user_agent=<user_agent>
```

URI parameters:

- `client_id`: Required. OAuth client id from the Fakturoid account settings.
- `client_secret`: Required. The matching client secret.
- `slug`: Required, never defaulted. The account slug as it appears in the Fakturoid URL. One set of credentials can reach several accounts, so guessing would silently load another account's books.
- `user_agent`: **Required.** Must carry a contact address, e.g. `MyCompany (billing@mycompany.com)`.
- `rate_limit`: Optional. Requests per second. Defaults to `1.5` (~90/min).

### The User-Agent is mandatory

Fakturoid rejects requests with a missing or generic `User-Agent` — it is a documented hard requirement, not a courtesy. It has no default here on purpose: a shared default would send one user's contact address on everyone else's traffic.

A bad User-Agent does **not** present as an auth error. It returns `403` on every endpoint, including `/oauth/token`, so it reads like a credential problem.

Authentication is OAuth2 `client_credentials`: the client id and secret are sent as HTTP Basic credentials to `POST /oauth/token`, which returns a bearer token valid for about two hours. Tokens are refreshed lazily and shared across requests, so a long backfill does not die halfway.

## Example usage

```bash
ingestr ingest \
  --source-uri 'fakturoid://?client_id=<id>&client_secret=<secret>&slug=<slug>&user_agent=MyCompany%20(billing@mycompany.com)' \
  --source-table invoices \
  --dest-uri duckdb:///fakturoid.duckdb \
  --dest-table main.invoices
```

## Tables

| Table | Primary key | Strategy | Data |
|---|---|---|---|
| `invoices` | `id` | merge | Invoices — 84 fields |
| `invoices_lines` | `invoice_id`, `id` | merge | Invoice line items, exploded from each invoice |
| `invoices_vat_rates` | `invoice_id`, `vat_rate` | merge | Per-invoice VAT-rate summaries |
| `subjects` | `id` | merge | Customers and suppliers — 49 fields |

`invoices_lines` and `invoices_vat_rates` are derived from the same `/invoices.json` payload as `invoices`, so requesting them costs a full re-page of the invoice list.

## Notes and limitations

### Pagination is fixed at 40 rows and there is no total count

`per_page` is not a parameter — the page size is a server constant. The only end-of-data signal is a page shorter than 40 rows.

### Field lists are allow-lists

Each table projects an explicit list of fields rather than everything the API returns. Fakturoid returns fields beyond those projected (`attachments`, `eet_records`, `legacy_bank_details`, `vat_rates_summary` on the invoice itself, and others), and emitting everything would make the destination shape follow whatever the vendor adds next.

Fields the API returns that are not in the list are counted and logged once per run as drift, so a vendor addition is visible rather than silently dropped.

### `merge` cannot see a deletion

Under the `merge` strategy on `(invoice_id, id)`, a line removed from an existing invoice lingers in the destination — the API simply stops returning it, and there is no tombstone. Invoice and subject deletions are equally invisible. If deletions matter for your use case, use a periodic full reload rather than an incremental one.

### Nested objects become JSON text

Nested objects are emitted as JSON text rather than structured columns, so the projection stays flat and destinations that handle nested types differently all receive the same shape.

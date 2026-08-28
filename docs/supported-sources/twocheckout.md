# 2Checkout

[2Checkout](https://www.2checkout.com/) (Verifone) is a payments and subscription-billing platform for digital goods and SaaS.

ingestr supports 2Checkout as a source through the [2Checkout REST API 6.0](https://verifone.cloud/docs/2checkout/API-Integration).

## URI format

```plaintext
twocheckout://?merchant_code=<merchant_code>&secret_key=<secret_key>
```

URI parameters:

- `merchant_code`: Required. The merchant code from the 2Checkout Control Panel.
- `secret_key`: Required. The API secret key issued alongside it.

The scheme is `twocheckout`, not `2checkout`: RFC 3986 URI schemes must begin with a letter.

Authentication is a custom HMAC-SHA256 signature in the `X-Avangate-Authentication` header, computed per request. The signature embeds a timestamp, so **the host clock must be accurate** — a skewed clock produces a `401` that looks like a bad credential.

## Example usage

```bash
ingestr ingest \
  --source-uri 'twocheckout://?merchant_code=<merchant_code>&secret_key=<secret_key>' \
  --source-table orders \
  --dest-uri duckdb:///twocheckout.duckdb \
  --dest-table main.orders
```

## Tables

| Table | Primary key | Incremental | Strategy | Data |
|---|---|---|---|---|
| `orders` | `RefNo`, `Status` | `StartDate` / `EndDate` | merge | Orders with amounts, status and customer reference |
| `subscriptions` | `SubscriptionReference` | `ModifiedAfter` | merge | Subscriptions and their lifecycle state |
| `products` | `ProductCode` | — | merge | Product catalogue |
| `promotions` | `code` | — | merge | Promotions and discount codes |

`--interval-start` / `--interval-end` bound the window for `orders` and `subscriptions`. The other two are snapshots.

There is no `customers` table. REST 6.0 exposes only `/customers/search/`, which requires an `Email` parameter, so there is no way to enumerate the customer base. Customer identity arrives on orders and subscriptions instead.

## Notes and limitations

### Orders are keyed on `(RefNo, Status)`, not `RefNo`

An order moves between statuses over its lifetime — `COMPLETE` → `REFUND` → `REVERSED`. 2Checkout **negates the money fields on a refund row**, so keying on `RefNo` alone would let `merge` overwrite the original charge with the refund and leave only negative amounts in the destination.

Keying on the pair keeps both rows, so a consumer can read the charge from the `COMPLETE` row and the refund from the `REFUND` row.

This does not recover history. `/orders/` returns each order's *current* state only, so a backfill can never re-observe a status an order has already left; transitions are preserved from the first load onwards.

### Gross amounts are corrected

The `/orders/` list endpoint serialises `GrossPrice` and `GrossDiscountedPrice` as a **running cumulative total across the page** — each order's gross is its own gross plus every gross before it in the response. This appears to be an upstream bug.

`NetPrice`, `NetDiscountedPrice` and `VAT` are correct, and `gross == net + vat` holds for every order including refunds (where all three are negative), so ingestr recomputes gross from those fields rather than ingesting the raw value. Loading the raw field would put silently, plausibly wrong revenue in the destination — the worst kind of wrong, because nothing looks broken.

The correction is guarded by an epsilon comparison, so it becomes a no-op if 2Checkout fixes the serialisation: a correct payload passes through untouched.

### `subscriptions` filters on `ModifiedAfter` only

`/subscriptions/` refuses to page past 20,000 results, so an unfiltered pull of a large account is impossible rather than merely slow. `StartDate`, `EndDate` and `ProductCode` are **silently ignored** by this endpoint — the returned `Count` is identical with and without them. `ModifiedAfter` is the only filter it honours.

That makes a narrow window return recently-*touched* subscriptions rather than recently-*created* ones, so the result cannot be read as "everything since date X". It is the right incremental key for `merge`, since it catches records that changed, but it is not a creation-date filter.

### Columns that are null everywhere

Schema inference drops a column that is null in every row of a batch, which silently removes fields like `Source` and `ExternalReference` from the destination. The source declares a minimum column set for `orders` so those fields get a type and survive. Declared columns are additive — every other key the API returns is still passed through.

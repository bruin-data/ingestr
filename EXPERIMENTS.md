# Stripe Performance Experiments

This file is an append-only record of Stripe source performance experiments. Benchmarks use a live restricted key, fixed UTC intervals, and the `discard://` destination so destination writes do not affect extraction time. Secrets are never stored here.

Pipeline durations are reported by ingestr and are more representative than process wall time, which includes CLI startup and telemetry shutdown. Live API latency varies, so repeated runs use the median unless noted otherwise.

## Experiment 0: Embedded subscription items and adaptive request governor

Date: 2026-08-06

Status: successful

### Changes

- Flatten subscription items embedded in subscription list responses.
- Fetch the per-subscription endpoint only when the embedded item list reports overflow.
- Enforce the global row limit across all subscriptions and propagate child errors.
- Add a credential-scoped request governor covering normal, streaming, multipart, form, and raw Stripe requests.
- Start live traffic at 100 requests/second with bursts of 20, 48 global in-flight requests, and 32 per endpoint.
- Reduce only the affected rate or concurrency budget using `Stripe-Rate-Limited-Reason`, then recover gradually.
- Preserve retry behavior while requiring every retry to reacquire governed capacity.
- Restore raw-request support through the wrapper. Without it, `payment_record` silently skipped every child fetch.
- Emit aggregate governor request, wait, and 429 statistics in debug mode.

### Benchmarks

#### Subscription items

Command shape: `subscription_item:sync`, `--sql-limit=200`, discard destination.

| Variant | Runs | Median | Requests | Governor wait | 429s |
|---|---:|---:|---:|---:|---:|
| Original implementation | User observation | about 6 rows/s | N+1 | n/a | n/a |
| Embedded implementation, governor 80/s | 3.6s, 4.4s, 3.6s | 3.6s (55.6 rows/s) | 2 | 0ms | 0 |
| Embedded implementation, governor 100/s | 3.4s, 5.2s, 4.3s | 4.3s (46.5 rows/s) | 2 | 0ms | 0 |

The difference between 80/s and 100/s is API latency noise: both variants made two requests and the governor delayed neither request. The structural N+1 removal, not the governor ceiling, produced the improvement.

#### Direct-list concurrency stress

Command shape: `balance_transaction:sync:incremental`, 2026-07-07 through 2026-08-06, extraction parallelism 120, 120 Stripe requests, 64 rows.

| Variant | Runs | Median | 429s |
|---|---:|---:|---:|
| Ungoverned | 1.6s (single run) | 1.6s | 12 `endpoint-concurrency` |
| Governor 80/s | 1.5s, 1.5s, 1.5s | 1.5s | 0 |
| Governor 100/s | 1.3s, 1.2s, 1.5s | 1.3s | 0 |

At saturation, 100/s was about 13% faster than 80/s and eliminated the ungoverned concurrency failures.

#### Short direct-list burst

Command shape: `balance_transaction:sync:incremental`, 2026-08-05 through 2026-08-06, extraction parallelism 30, 24 Stripe requests, 3 rows.

| Variant | Runs | Median | 429s |
|---|---:|---:|---:|
| Ungoverned | 0.5s (single run) | 0.5s | 0 |
| Governor 80/s | 0.7s, 0.5s, 0.6s | 0.6s | 0 |
| Governor 100/s | 0.6s, 0.4s, 0.4s | 0.4s | 0 |

Durations are rounded to 100ms, but the aggregate pacing delay fell from about 129ms at 80/s to 103ms at 100/s.

#### Nested payment-record requests

Command shape: `payment_record:sync:incremental`, 2026-07-07 through 2026-08-06, extraction parallelism 120, 149 Stripe requests.

| Variant | Runs | Median | 429s | Result |
|---|---:|---:|---:|---|
| Previous wrapper | 2.0s (single run) | invalid | many | Raw child requests were unsupported and skipped |
| Governor 80/s | 1.8s, 2.0s, 1.9s | 1.9s | 0 | 120 parent requests plus 29 real child requests |
| Governor 100/s | 1.5s, 1.8s, 1.7s | 1.7s | 0 | 120 parent requests plus 29 real child requests |

All 29 child requests returned the expected `resource_missing` response because the sampled PaymentIntents had no PaymentRecord. The important result is that raw requests now execute, are governed, and do not produce concurrency 429s.

### Validation

- `make format`
- `make lint`
- `make test`
- `go test -race ./pkg/source/stripe`

### Conclusion

Keep the embedded subscription-item reader and the 100/s adaptive governor. The governor preserves normal-path performance, improves saturated extraction, and prevents retry storms. Subscription-item speed is governed by Stripe response latency after reducing the operation to two requests.

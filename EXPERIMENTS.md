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

## Experiment 1: Central request correctness and endpoint telemetry

Date: 2026-08-06

Status: successful

### Changes

- Clamp every Stripe list request to Stripe's maximum page size of 100, including the CLI default of 25,000.
- Trim a final page before Arrow conversion when a reader-level limit ends inside that page.
- Reject a malformed `has_more` response without a cursor instead of looping forever.
- Make result delivery cancellation-aware so downstream cancellation releases Arrow records and stops pagination.
- Propagate the read context into list, object-refetch, account, parent-child, and oldest-record probe requests.
- Record request count, errors, 429s, governed waits, and average API latency for each normalized Stripe endpoint.

### Benchmarks

#### Direct-list concurrency stress

Command shape: `balance_transaction:sync:incremental`, 2026-07-07 through 2026-08-06, extraction parallelism 120, discard destination.

| Variant | Runs | Median | Requests | 429s | Average API latency |
|---|---:|---:|---:|---:|---:|
| Experiment 0, governor 100/s | 1.3s, 1.2s, 1.5s | 1.3s | 120 | 0 | not recorded per endpoint |
| Centralized request path | 1.2s, 1.8s, 1.2s | 1.2s | 120 | 0 | 290ms, 408ms, 289ms |

The median improved by 0.1s, but the difference is normal API-latency noise. The successful result is no throughput regression, the same 64 rows and 120 requests, no 429s, and endpoint-level evidence showing that Stripe response time—not local page processing—dominates this workload.

#### Page-size safety

Command shape: one-day `balance_transaction:sync:incremental` read with `--page-size=25000`.

The live debug trace reported `batch size: 100` for every time window. Unit coverage also verifies negative, zero, sub-maximum, maximum, and over-maximum values. This prevents Stripe from rejecting ingestr's generic 25,000-row page-size default while preserving smaller explicit page sizes.

### Validation

- `go test -race ./pkg/source/stripe`
- `make format`
- `make lint`
- `make test`

### Conclusion

Keep the centralized page cap, context propagation, pagination guards, and endpoint telemetry. They make cancellation and pagination reliable without changing the emitted objects, and they make subsequent optimization decisions attributable to a concrete Stripe endpoint.

## Experiment 2: Bounded payment-method fan-out

Date: 2026-08-06

Status: successful

### Changes

- Replace the serial customer-to-payment-method loop with a bounded worker pool.
- Use the configured extraction parallelism, defaulting to 10 only when no value is supplied and capping the fan-out at the governor's 32-request endpoint concurrency ceiling.
- Stream each customer's pages as soon as they arrive; no account data is buffered.
- Cancel sibling work and return the first child error instead of logging it and silently producing an incomplete table.

### Benchmarks

Command shape: full `payment_method:sync` load, discard destination. The live account had 455 customers and 63 payment methods; every run made five customer-page requests and 455 payment-method requests.

| Variant | Runs | Median | Rows | Requests | Errors / 429s |
|---|---:|---:|---:|---:|---:|
| Serial committed baseline | 114.0s | 114.0s | 63 | 460 | 0 / 0 |
| Worker pool, default parallelism 5 | 19.2s, 20.3s, 19.2s | 19.2s | 63 | 460 | 0 / 0 |
| Worker pool, parallelism 20 | 5.6s, 6.0s, 5.1s | 5.6s | 63 | 460 | 0 / 0 |

The default load is 5.9× faster. At parallelism 20 it is 20.4× faster, while the governor introduced at most 119ms of aggregate wait and Stripe returned no rate-limit responses. Row count and request count remained identical, demonstrating that concurrency changed scheduling but not table contents.

### Validation

- `go test -race ./pkg/source/stripe`
- `make format`
- `make lint`
- `make test`

### Conclusion

Keep the bounded worker pool. The normal default removes most serial latency, while `--extract-parallelism=20` makes substantially better use of the live account's available request capacity without approaching the governor's endpoint-concurrency ceiling.

## Experiment 3: Bounded setup-attempt fan-out

Date: 2026-08-06

Status: successful

### Changes

- Fetch setup attempts for independent SetupIntents through the same bounded, configurable worker model.
- Keep time filtering on each child request exactly as before.
- Stream child pages immediately and cancel sibling work on the first error.
- Return child failures instead of logging them and silently completing with missing rows.

### Benchmarks

Command shape: full `setup_attempt:sync` load, discard destination. The live account produced 74 setup attempts from 597 SetupIntents; every run made six parent-page requests and 597 child requests.

| Variant | Runs | Median | Rows | Requests | Errors / 429s |
|---|---:|---:|---:|---:|---:|
| Serial committed baseline | 146.0s | 146.0s | 74 | 603 | 0 / 0 |
| Worker pool, default parallelism 5 | 30.2s, 29.7s, 30.4s | 30.2s | 74 | 603 | 0 / 0 |
| Worker pool, parallelism 20 | 9.4s, 9.5s, 9.5s | 9.5s | 74 | 603 | 0 / 0 |

The normal default is 4.8× faster and parallelism 20 is 15.4× faster. At parallelism 20 the governor delayed at most three requests for 28ms total; the API returned no 429s. Identical row and request counts confirm unchanged table contents.

### Validation

- `go test -race ./pkg/source/stripe`
- `make format`
- `make lint`
- `make test`

### Conclusion

Keep the bounded setup-attempt worker pool. This table had the largest serial request fan-out observed so far, and concurrency converts nearly all of that independent network wait into useful throughput without changing the response payloads or filters.

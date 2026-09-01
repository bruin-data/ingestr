# RIPEstat

[RIPEstat](https://stat.ripe.net/) provides Internet routing, registration, geography, DNS, and RPKI data from the RIPE NCC. Its Data API is public and does not require authentication.

## URI format

The source URI has no parameters because the API is public:

```plaintext
ripestat://
```

Use the RIPEstat endpoint name as the source table and append the endpoint's [request parameters](https://stat.ripe.net/docs/data-api/ripestat-data-api) in URL query format. For example, this command loads an overview of AS3333 into DuckDB:

```sh
ingestr ingest \
  --source-uri 'ripestat://' \
  --source-table 'as-overview?resource=AS3333' \
  --dest-uri 'duckdb:///ripestat.duckdb' \
  --dest-table 'main.as_overview'
```

Other source table examples include `announced-prefixes?resource=AS3333`, `prefix-overview?resource=193.0.20.0%2F24`, and `routing-history?resource=AS3333`. Endpoints that take no parameters, such as `example-resources`, use only the endpoint name.

## Time intervals

For endpoints that support a bounded time range, use ingestr's interval flags. They are sent to RIPEstat as `starttime` and `endtime` in UTC:

```sh
ingestr ingest \
  --source-uri 'ripestat://' \
  --source-table 'announced-prefixes?resource=AS3333' \
  --interval-start '2026-01-01' \
  --interval-end '2026-02-01' \
  --dest-uri 'duckdb:///ripestat.duckdb' \
  --dest-table 'main.announced_prefixes'
```

The interval flags are supported by `allocation-history`, `announced-prefixes`, `asn-neighbours-history`, `atlas-probe-deployment`, `bgp-update-activity`, `bgp-updates`, `bgplay`, `country-resource-stats`, `prefix-count`, `rir`, `ris-peer-count`, and `routing-history`. Supplying them for another endpoint returns an error instead of silently ignoring the interval. If `starttime` or `endtime` are also present in the source table, the interval flags take precedence.

Loads default to the `replace` strategy. `--incremental-strategy` and `--primary-key` can be used when snapshots should instead be appended or merged. RIPEstat has no common incremental key across endpoints, so `--incremental-key` is not used for API filtering.

Each request produces one snapshot row from the endpoint's `data` object. Scalar properties become columns, while nested objects and arrays are stored as JSON.

RIPEstat limits callers to eight concurrent requests per IP address. A source read makes one request at a time and does not paginate or parallelize requests.

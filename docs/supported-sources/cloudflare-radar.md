# Cloudflare Radar

[Cloudflare Radar](https://radar.cloudflare.com/) provides aggregated Internet traffic, routing, outage, bot, and certificate transparency data. ingestr supports Cloudflare Radar as a source.

## URI format

```plaintext
cloudflare-radar://?api_token=<api-token>
```

Create an API token that can read Cloudflare Radar data. Radar's data endpoints are global, so the Cloudflare account ID is not part of the source URI.

## Named tables

The connector provides aliases for commonly loaded catalog and event datasets:

| Table | Primary key | Description |
| --- | --- | --- |
| `annotations` | `id` | Internet events, outages, and traffic anomalies published by Radar. |
| `autonomous_systems` | `asn` | Autonomous systems known to Radar. |
| `bgp_hijacks` | `id` | Detected BGP route hijack events. |
| `bgp_leaks` | `id` | Detected BGP route leak events. |
| `bots` | `slug` | Radar's catalog of known bots and their user-agent patterns. |
| `certificate_authorities` | `sha256Fingerprint` | Certificate authorities tracked through Certificate Transparency. |
| `certificate_logs` | `slug` | Certificate Transparency logs and their current states. |
| `datasets` | `id` | Downloadable Radar ranking and report datasets. |
| `geolocations` | `geoId` | Radar's geographic location catalog. |
| `locations` | `alpha2` | Countries and regions available as Radar filters. |
| `outages` | `id` | Internet outage annotations. |
| `origins` | `slug` | Origins available in Radar's origin metrics. |
| `tlds` | `tld` | Top-level domains and their managers. |
| `traffic_anomalies` | `uuid` | Recent Internet traffic anomalies. |

Named collection tables are automatically paginated. `datasets` loads both ranking-bucket and report datasets.

```sh
ingestr ingest \
    --source-uri 'cloudflare-radar://?api_token=your-api-token' \
    --source-table 'annotations' \
    --dest-uri 'duckdb:///radar.duckdb' \
    --dest-table 'main.annotations'
```

`annotations`, `outages`, `bgp_hijacks`, `bgp_leaks`, and `traffic_anomalies` accept `--interval-start` and `--interval-end`. Without an interval, annotations and outages cover the maximum API-supported trailing range of 364 days; BGP event tables return all events exposed by the API; traffic anomalies cover the trailing seven days.

## All Radar API endpoints

Every GET endpoint in the [Cloudflare Radar API](https://developers.cloudflare.com/api/resources/radar/) is available as a dynamic table. Set `--source-table` to `api:` followed by the endpoint path after `/radar/`. Add the endpoint's query parameters directly to the table name.

For example, the API route `/radar/http/timeseries` becomes:

```sh
ingestr ingest \
    --source-uri 'cloudflare-radar://?api_token=your-api-token' \
    --source-table 'api:http/timeseries?dateRange=7d&location=US' \
    --dest-uri 'duckdb:///radar.duckdb' \
    --dest-table 'main.http_requests'
```

Parameterized route segments are filled in as part of the path:

```sh
# /radar/http/summary/{dimension}
--source-table 'api:http/summary/http_protocol?dateRange=7d'

# /radar/entities/asns/{asn}
--source-table 'api:entities/asns/13335'

# /radar/ranking/domain/{domain}
--source-table 'api:ranking/domain/cloudflare.com'
```

This covers all Radar families, including agent readiness, AI, annotations, AS112, attacks, BGP, bots, Certificate Transparency, datasets, DNS, email, entities, geolocations, HTTP, leaked credential checks, netflows, origins, post-quantum, quality, ranking, robots.txt, search, TCP resets and timeouts, TLDs, and traffic anomalies. Because the endpoint path and parameters are passed through, newly added Radar GET endpoints work without a connector release as long as they use Radar's standard JSON response envelope or return CSV.

The source table must be shell-quoted because query strings contain `&`. Repeated parameters are supported, for example `location=US&location=GB`. JSON is used for normal API responses. The `/radar/datasets/{alias}` download endpoint returns CSV, which the connector parses into one row per record.

Dynamic list endpoints are automatically paginated when Radar exposes offset or page pagination. Analytics, top, detail, search, and other non-list endpoints make one request using the supplied parameters. Use the endpoint's `limit` parameter where applicable.

### Response rows

Dynamic endpoint responses are converted to relational rows as follows:

- list and top responses produce one row per item;
- time-series responses produce one row per timestamp, with each metric as a column;
- grouped or multiple series include `_series` when needed;
- summary responses produce `dimension` and `value` columns;
- detail responses produce one row;
- nested values and Radar response metadata remain JSON columns.

Dynamic tables default to the `replace` strategy. To merge them, provide the endpoint's stable key with `--primary-key` and select the merge strategy. `--interval-start` and `--interval-end` are forwarded as `dateStart` and `dateEnd` unless those parameters are already present in the source-table query string.

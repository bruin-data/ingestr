# PeeringDB

[PeeringDB](https://www.peeringdb.com/) is a freely available, user-maintained database of networks, Internet Exchange Points (IXPs), data centers, and the interconnections between them.

## URI format

The PeeringDB API is public, so the API key is optional:

```plaintext
peeringdb://
peeringdb://?api_key=<api_key>
```

Without a key, ingestr reads PeeringDB as a guest. With a [PeeringDB API key](https://docs.peeringdb.com/howto/api_keys/), ingestr gets a higher query allowance and can read contact records that are not public. PeeringDB throttles guests per IP address, so without a key, load tables one at a time. PeeringDB data is subject to its [acceptable use policy](https://www.peeringdb.com/aup).

## Example

This command loads every network into DuckDB:

```sh
ingestr ingest \
  --source-uri 'peeringdb://' \
  --source-table 'networks' \
  --dest-uri 'duckdb:///peeringdb.duckdb' \
  --dest-table 'main.networks'
```

## Tables

| Table | PeeringDB object | Description |
| --- | --- | --- |
| `organizations` | `org` | Organizations that own networks, exchanges, facilities, carriers, and campuses |
| `campuses` | `campus` | Campuses that group facilities |
| `facilities` | `fac` | Facilities, such as data centers |
| `carriers` | `carrier` | Carriers that provide connectivity between facilities |
| `carrier_facilities` | `carrierfac` | Carrier presence at facilities |
| `exchanges` | `ix` | Internet Exchange Points |
| `exchange_facilities` | `ixfac` | Exchange presence at facilities |
| `exchange_lans` | `ixlan` | Exchange peering LANs |
| `exchange_prefixes` | `ixpfx` | Prefixes used on exchange peering LANs |
| `networks` | `net` | Networks (autonomous systems) |
| `network_facilities` | `netfac` | Network presence at facilities |
| `network_exchange_lans` | `netixlan` | Network connections to exchanges, with IP addresses and port speeds |
| `network_contacts` | `poc` | Network points of contact; guests only see contacts marked public |

Every table uses `id` as its primary key and is loaded with the `merge` strategy, using `updated` as the incremental key. Pass `--interval-start` and `--interval-end` to load only records updated in that range.

Records deleted in PeeringDB are kept with `status` set to `deleted`, so merges can mark them. Nested values, such as `social_media` and `info_types`, are stored as JSON.

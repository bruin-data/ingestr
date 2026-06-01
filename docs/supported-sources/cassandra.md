# Cassandra

[Apache Cassandra](https://cassandra.apache.org/) is a distributed wide-column database.

ingestr supports Cassandra as both a source and a destination.

## URI format

```plaintext
cassandra://user:password@host:9042/keyspace
```

URI parameters:

- `user`: the username to connect with, optional
- `password`: the password for the user, optional
- `host`: one or more seed hosts. Multiple hosts can be provided with `?hosts=host1,host2`
- `port`: the Cassandra native transport port, default is `9042`
- `keyspace`: the default keyspace, either in the URI path or as `?keyspace=...`

Optional query parameters:

- `consistency`: one of `one`, `quorum`, `local_quorum`, `all`, and other Cassandra consistency names
- `page_size`: source read page size
- `timeout` and `connect_timeout`: Go duration strings such as `10s`
- `ssl=true`: enable TLS
- `disable_initial_host_lookup=true`: useful for single-node Docker or NAT setups
- `replication_factor`: destination keyspace creation replication factor, default is `1`

The same URI structure can be used for sources and destinations.

## Source table format

Use either a plain table name when the URI includes a keyspace:

```plaintext
events
```

or a fully qualified table name:

```plaintext
analytics.events
```

Custom CQL queries are also supported:

```plaintext
query:SELECT id, event_type, created_at FROM analytics.events WHERE created_at >= :interval_start ALLOW FILTERING
```

## Example: Load from Cassandra

```sh
ingestr ingest \
  --source-uri "cassandra://localhost:9042/analytics?disable_initial_host_lookup=true" \
  --source-table "events" \
  --dest-uri "duckdb:///analytics.duckdb" \
  --dest-table "events"
```

## Example: Load into Cassandra

Cassandra requires a primary key when creating destination tables. The first primary key is used as the partition key and additional keys are used as clustering keys.

```sh
ingestr ingest \
  --source-uri "postgres://user:pass@localhost:5432/app" \
  --source-table "public.events" \
  --dest-uri "cassandra://localhost:9042/analytics?disable_initial_host_lookup=true" \
  --dest-table "events" \
  --incremental-strategy "merge" \
  --primary-key "id"
```

Supported destination strategies are `replace`, `append`, `merge`, and `truncate+insert`. `merge` uses Cassandra upsert semantics. `delete+insert` and `scd2` are not supported.

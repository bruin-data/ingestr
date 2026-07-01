# PlanetScale
PlanetScale is a managed MySQL-compatible database built on Vitess.

ingestr supports PlanetScale as both a source and destination through the MySQL connector. PlanetScale change data capture is also supported, through PlanetScale's hosted `psdbconnect` API.

## URI format
The URI format for PlanetScale is as follows:

```plaintext
mysql://user:password@host:port/database?tls=true
```

URI parameters:
- `user`: the user name to connect to the database
- `password`: the password for the user
- `host`: the PlanetScale database host
- `port`: the MySQL protocol port, usually 3306
- `database`: the PlanetScale database name
- `tls=true`: required for PlanetScale connections

Use the same URI structure for sources and destinations. PlanetScale uses the MySQL wire protocol, so the URI scheme remains `mysql://`.

Example:

```shell
ingestr ingest \
  --source-uri "mysql://user:password@host:3306/database?tls=true" \
  --dest-uri "duckdb:///tmp/planetscale.duckdb" \
  --source-table "orders" \
  --dest-table "orders"
```

To load into PlanetScale, use the PlanetScale URI as the destination:

```shell
ingestr ingest \
  --source-uri "postgresql://user:password@host:5432/app" \
  --dest-uri "mysql://user:password@host:3306/database?tls=true" \
  --source-table "public.orders" \
  --dest-table "orders"
```

## Change data capture
PlanetScale change data capture uses the `mysql+cdc://` URI scheme. PlanetScale does not expose vtgate's VStream gRPC port to external clients, so ingestr streams changes through PlanetScale's hosted `psdbconnect` API on the database host over TLS (port 443). It authenticates with the **database credentials already in the URI** — the same `user:password` used for the MySQL connection — so no separate token is required.

ingestr selects this path automatically when the host ends in `.psdb.cloud`; for private endpoints or custom domains, force it with `cdc_backend=planetscale`. Keep `tls=true` (required for both the MySQL-protocol schema discovery and the psdbconnect endpoint). The database in the URI is the PlanetScale keyspace. Unlike self-hosted Vitess, there is no `grpc_port` — the psdbconnect endpoint is always the database host on port 443.

Example:

```shell
ingestr ingest \
  --source-uri "mysql+cdc://user:password@host.connect.psdb.cloud:3306/database?tls=true" \
  --dest-uri "duckdb:///tmp/planetscale_cdc.duckdb" \
  --source-table "orders" \
  --dest-table "orders"
```

psdbconnect performs a per-shard snapshot first (resumable by primary key) and then streams inserts, updates, and deletes. Position is tracked per shard and serialized into `_cdc_lsn`, and subsequent runs resume from the destination table's maximum `_cdc_lsn` for both unsharded and sharded keyspaces. If the stored `_cdc_lsn` is invalid, the run fails instead of taking a partial snapshot — run with `--full-refresh` to rebuild the destination from a fresh snapshot. Incremental runs use the `merge` strategy so updates and deletes are applied by primary key. (PlanetScale delivers only the primary keys of deleted rows; the destination marks them deleted without disturbing the other columns.)

CDC URI parameters:
- `tls`: keep `tls=true` — required for the connection.
- `cdc_backend`: optional override — `planetscale` forces the psdbconnect path (for example on a custom domain or private endpoint), `vstream` forces the self-hosted Vitess VStream path.
- `dest_schema`: optional destination schema for multi-table CDC runs.

Requirements:
- PlanetScale database credentials (`user:password`) with read access to the branch/keyspace.
- Source tables must have primary keys, or `--primary-key` must be provided.
- Source tables must not contain `ENUM`, `SET`, or `BIT` columns.

## Related docs
- [MySQL](/supported-sources/mysql.md) for the generic MySQL connector, TLS options, and binary-log / Vitess VStream CDC.

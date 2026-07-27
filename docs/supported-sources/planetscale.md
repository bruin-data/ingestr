# PlanetScale
PlanetScale is a managed MySQL-compatible database built on Vitess.

ingestr supports PlanetScale as both a source and destination through its dedicated `ps_mysql://` scheme. PlanetScale change data capture is also supported, through PlanetScale's hosted `psdbconnect` API.

## URI format
The URI format for PlanetScale is as follows:

```plaintext
ps_mysql://user:password@host:port/database
```

URI parameters:
- `user`: the user name to connect to the database
- `password`: the password for the user
- `host`: the PlanetScale database host
- `port`: the MySQL protocol port, usually 3306
- `database`: the PlanetScale database name (keyspace)

Use the same URI structure for sources and destinations. PlanetScale speaks the MySQL wire protocol, but it is selected with the `ps_mysql://` scheme (not `mysql://`) so ingestr uses the PlanetScale-aware read, write, and CDC paths. Pointing a `mysql://` URI at a PlanetScale server fails fast with a message telling you to use `ps_mysql://`.

## TLS
PlanetScale requires encrypted connections and rejects plaintext ones with `server does not allow insecure connections, client must use SSL/TLS`. ingestr enables TLS automatically for the `ps_mysql://` scheme — for **both source and destination** connections (and the MySQL-protocol connection the CDC path uses for schema discovery) — so you do not need to add `?tls=true` yourself.

You can still set `tls` explicitly when you need to:
- `tls=true`: connect over TLS and verify the server certificate against the system roots (this is the auto-enabled default).
- `tls=skip-verify`: connect over TLS without verifying the certificate.

Example:

```shell
ingestr ingest \
  --source-uri "ps_mysql://user:password@aws.connect.psdb.cloud:3306/database" \
  --dest-uri "duckdb:///tmp/planetscale.duckdb" \
  --source-table "orders" \
  --dest-table "orders"
```

To load into PlanetScale, use the PlanetScale URI as the destination (TLS is enabled automatically):

```shell
ingestr ingest \
  --source-uri "postgresql://user:password@host:5432/app" \
  --dest-uri "ps_mysql://user:password@aws.connect.psdb.cloud:3306/database" \
  --source-table "public.orders" \
  --dest-table "orders"
```

When loading into PlanetScale, keep two things in mind:

- **Direct DDL must be allowed on the target branch.** The `replace` strategy and any table creation issue `CREATE` / `RENAME` statements. On a branch with safe migrations enabled, PlanetScale rejects these with `ERROR 1105 (HY000): direct DDL is disabled` — load into a development branch (or a branch with safe migrations off) instead, or pre-create the tables and use `append`/`merge`. The PlanetScale database (keyspace) must already exist; ingestr does not create it.
- **Only unsharded keyspaces are supported** as destinations. A sharded keyspace is detected at connect and rejected with a clear error. See [Vitess as a destination](/supported-sources/vitess.md#vitess-as-a-destination) for the underlying reasons.

## Change data capture
PlanetScale change data capture uses the `ps_mysql+cdc://` URI scheme. PlanetScale does not expose vtgate's VStream gRPC port to external clients, so ingestr streams changes through PlanetScale's hosted `psdbconnect` API on the database host over TLS (port 443). It authenticates with the **database credentials already in the URI** — the same `user:password` used for the MySQL connection — so no separate token is required.

TLS is required, and ingestr enables it automatically for the `ps_mysql://` scheme (see [TLS](#tls)), so you don't need to add `tls=true` — the psdbconnect endpoint always uses TLS on port 443 regardless. The database in the URI is the PlanetScale keyspace. Unlike self-hosted Vitess, there is no `grpc_port` — the psdbconnect endpoint is always the database host on port 443.

Example:

```shell
ingestr ingest \
  --source-uri "ps_mysql+cdc://user:password@aws.connect.psdb.cloud:3306/database" \
  --dest-uri "duckdb:///tmp/planetscale_cdc.duckdb" \
  --source-table "orders" \
  --dest-table "orders"
```

psdbconnect performs a per-shard snapshot first (resumable by primary key) and then streams inserts, updates, and deletes. Position is tracked per shard and serialized into `_cdc_lsn`, and subsequent runs resume from the destination table's maximum `_cdc_lsn` for both unsharded and sharded keyspaces. If the stored `_cdc_lsn` is invalid, the run fails instead of taking a partial snapshot — run with `--full-refresh` to rebuild the destination from a fresh snapshot. Incremental runs use the `merge` strategy so updates and deletes are applied by primary key. (PlanetScale delivers only the primary keys of deleted rows; the destination marks them deleted without disturbing the other columns.)

CDC URI parameters:
- `tls`: auto-enabled for the `ps_mysql://` scheme; set it explicitly only to choose a different mode (see [TLS](#tls)).
- `dest_schema`: optional destination schema for multi-table CDC runs. Ignored when `--source-table` is set; the destination is then `--dest-table`.

Requirements:
- PlanetScale database credentials (`user:password`) with read access to the branch/keyspace.
- Source tables must have primary keys, or `--primary-key` must be provided.
- Source tables must not contain `ENUM`, `SET`, or `BIT` columns.

## Related docs
- [MySQL](/supported-sources/mysql.md) for the generic MySQL connector and binary-log CDC.
- [Vitess](/supported-sources/vitess.md) for self-hosted Vitess and its VStream CDC path.

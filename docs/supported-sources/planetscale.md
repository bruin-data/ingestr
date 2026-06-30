# PlanetScale
PlanetScale is a managed MySQL-compatible database built on Vitess.

ingestr supports PlanetScale as both a source and destination through the MySQL connector. PlanetScale change data capture is also supported through Vitess VStream.

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
PlanetScale CDC uses the `mysql+cdc://` URI scheme. ingestr detects PlanetScale as a Vitess-compatible server and streams changes through vtgate's [VStream](https://vitess.io/docs/reference/vreplication/vstream/) API over gRPC.

```plaintext
mysql+cdc://user:password@host:3306/database?grpc_port=<grpc-port>&mode=batch&tls=true
```

CDC over PlanetScale opens two connections:
- the MySQL protocol connection for schema discovery
- the vtgate gRPC connection for the change stream

The `tls=true` parameter secures both connections. The gRPC connection inherits the `tls` setting, so PlanetScale CDC usually needs only `grpc_port` plus `tls=true`.

Example:

```shell
ingestr ingest \
  --source-uri "mysql+cdc://user:password@host:3306/database?grpc_port=15991&mode=batch&tls=true" \
  --dest-uri "duckdb:///tmp/planetscale_cdc.duckdb" \
  --source-table "orders" \
  --dest-table "orders"
```

CDC URI parameters:
- `grpc_port`: required vtgate gRPC port. This is different from the MySQL protocol port and cannot be derived automatically.
- `grpc_host`: optional vtgate gRPC host; defaults to the host in the URI.
- `grpc_tls`: optional override for the gRPC connection's TLS, independent of `tls`. `true` verifies the server certificate, `skip-verify` skips verification, and `false` forces plaintext.
- `mode`: `batch`; defaults to `batch`.
- `dest_schema`: optional destination schema for multi-table CDC runs.

Requirements:
- The vtgate gRPC endpoint must be reachable.
- Source tables must have primary keys, or `--primary-key` must be provided.
- Source tables must not contain `ENUM`, `SET`, or `BIT` columns.

PlanetScale CDC performs a consistent copy-phase snapshot first, then streams changes. Position is tracked in `_cdc_lsn`, and subsequent runs resume from the destination table's maximum `_cdc_lsn`. If the stored `_cdc_lsn` is invalid, the run fails instead of taking a partial snapshot. Run with `--full-refresh` to rebuild the destination from a fresh snapshot.

Incremental CDC runs use the `merge` strategy so updates and deletes can be applied by primary key.

## Related docs
- [MySQL](/supported-sources/mysql.md) for the generic MySQL connector and binary-log CDC behavior.
- [Vitess VStream](https://vitess.io/docs/reference/vreplication/vstream/) for the underlying change stream API.

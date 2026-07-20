# Vitess
[Vitess](https://vitess.io/) is a database clustering system for horizontal scaling of MySQL. ingestr supports Vitess as both a source and destination, plus change data capture through vtgate's VStream API.

Vitess speaks the MySQL wire protocol, but it is selected with its own `vitess://` scheme (not `mysql://`) so ingestr uses the Vitess-aware read, write, and CDC paths. Pointing a `mysql://` URI at a Vitess server fails fast with a message telling you to use `vitess://`.

> [!NOTE]
> [PlanetScale](/supported-sources/planetscale.md) is managed Vitess, but it has its own hosted CDC path and is documented separately under the `ps_mysql://` scheme.

## URI format
```plaintext
vitess://user:password@host:port/keyspace
```

URI parameters:
- `user`: the vtgate user name
- `password`: the password for the user
- `host`: the vtgate MySQL-protocol host
- `port`: the vtgate MySQL-protocol port, usually 3306
- `keyspace`: the Vitess keyspace (used as the database)

If your vtgate requires encrypted MySQL-protocol connections, add `?tls=true`.

By default Vitess caps queries at 100,000 rows in its OLTP workload, which would otherwise break bulk reads of larger tables. The Vitess source runs in the OLAP workload, so large tables ingest fully.

## Vitess as a destination
Vitess is also supported as a destination over the `vitess://` URI. Two things differ from plain MySQL:

- **The target keyspace must already exist.** `CREATE DATABASE` is not supported through vtgate, so ingestr never creates keyspaces — create the keyspace via your Vitess control plane first. Staging tables for the `replace`, `merge`, `delete+insert`, and `scd2` strategies are created inside the target keyspace rather than in a separate `_bruin_staging` database.
- **Only unsharded (single-shard) keyspaces are supported.** If the target keyspace is sharded, ingestr fails fast at connect with a clear error instead of producing a broken load. Sharded keyspaces are unsupported because auto-created tables need a Primary Vindex to be routable, the `merge`/`delete+insert`/`scd2` strategies use `UPDATE … JOIN` / `INSERT … SELECT … WHERE NOT EXISTS` statements that Vitess rejects across shards, and the atomic `RENAME` swap used by `replace` is not atomic across shards. To load a sharded keyspace, pre-create the tables (with vindexes) and manage the load outside ingestr.

## Change data capture
Vitess CDC uses the `vitess+cdc://` scheme. ingestr streams changes through vtgate's [VStream](https://vitess.io/docs/reference/vreplication/vstream/) API over gRPC — Vitess is a sharded layer with no standard binary log to tail. It produces the same `_cdc_lsn`, `_cdc_deleted`, and `_cdc_synced_at` metadata columns as the other CDC sources and resumes from the destination table's maximum `_cdc_lsn` on subsequent runs.

VStream performs a consistent copy-phase snapshot first, then streams changes. Position is tracked with a Vitess GTID (VGTID) serialized into `_cdc_lsn`. This works for both unsharded and sharded keyspaces, since the VGTID covers every shard. If the stored `_cdc_lsn` is invalid, the run fails instead of taking a partial snapshot — run with `--full-refresh` to rebuild. Incremental runs use the `merge` strategy so updates and deletes are applied by primary key.

VStream uses vtgate's **gRPC** port, which is different from the MySQL protocol port and cannot be derived from it, so you must supply it with `grpc_port`. The database in the URI is the Vitess keyspace.

CDC over Vitess opens two connections: the MySQL protocol connection for schema discovery and the vtgate gRPC port for the change stream. A single `tls=true` secures both connections because the gRPC connection inherits the `tls` setting. To control the gRPC side independently, use `grpc_tls` (see below).

```shell
ingestr ingest \
  --source-uri "vitess+cdc://user:password@host:3306/keyspace?grpc_port=15991" \
  --dest-uri "duckdb:///tmp/vitess_cdc.duckdb" \
  --source-table "keyspace.orders" \
  --dest-table "orders"
```

Vitess CDC URI parameters:
- `grpc_port`: **required** — the vtgate gRPC port (for example `15991`). The run fails with a clear error if it is missing.
- `grpc_host`: optional vtgate gRPC host; defaults to the host in the URI.
- `grpc_tls`: optional override for the gRPC connection's TLS, independent of `tls`. `true` verifies the server certificate, `skip-verify` skips verification, `false` forces plaintext. When omitted, the gRPC connection inherits `tls` (`true`/`skip-verify` enable it; `preferred` and custom CA names do not).
- `mode`: `batch`; defaults to `batch`.
- `dest_schema`: optional destination schema for multi-table CDC runs. Ignored when `--source-table` is set; the destination is then `--dest-table`.

Requirements:
- The vtgate gRPC endpoint must be reachable (`grpc_port`, plus `grpc_host` if it differs from the MySQL host).
- Source tables must have primary keys, or `--primary-key` must be provided.
- Source tables must not contain `ENUM`, `SET`, or `BIT` columns.

## Related docs
- [MySQL](/supported-sources/mysql.md) for the generic MySQL/MariaDB connector and binary-log CDC.
- [PlanetScale](/supported-sources/planetscale.md) for managed Vitess with hosted `psdbconnect` CDC.

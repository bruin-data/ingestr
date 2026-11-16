# Oracle
Oracle is a powerful, fully integrated stack of cloud applications and platform services, known for its comprehensive capabilities in database management.

ingestr supports Oracle as a source and destination through the `oracle+cx_oracle`-compatible URI format. Under the hood, ingestr uses the Go Oracle driver in thin mode, which means **no Oracle Client libraries are required** to be installed on your system.

Oracle Exadata is also supported as a destination. Exadata uses the same Oracle Database protocol and SQL dialect, so it shares Oracle's destination implementation and does not require a separate client library.

## URI format
The URI format for Oracle is as follows:

```plaintext
oracle+cx_oracle://user:password@host:port/dbname
```

URI parameters:
- `user`: the user name to connect to the database
- `password`: the password for the user
- `host`: the host address of the database server
- `port`: the port number the database server is listening on, the default is 1521 for Oracle databases
- `dbname`: the name of the database

The same URI structure can be used both for sources and destinations. You can read more about SQLAlchemy's Oracle dialect [here](https://docs.sqlalchemy.org/en/20/core/engines.html#oracle).

### TNS connect descriptor

When a database is only reachable through a full TNS connect descriptor, put the descriptor where the host would go:

```plaintext
oracle://user:password@(DESCRIPTION=(ADDRESS=(PROTOCOL=TCP)(HOST=db.example.com)(PORT=1521))(CONNECT_DATA=(SERVICE_NAME=ORCLPDB)))
```

ingestr sends the descriptor to the listener as given, so `ADDRESS_LIST`, `FAILOVER`, `LOAD_BALANCE`, `PROTOCOL=TCPS`, and other descriptor settings are honored. Line breaks and indentation inside the descriptor are allowed. Driver options can follow the descriptor as a query string, e.g. `...)))?SSL VERIFY=false`.

The descriptor can also be passed URL-encoded in the `dsn` query parameter, which is useful when it contains characters such as `&` or `#`:

```plaintext
oracle://user:password@/?dsn=%28DESCRIPTION%3D%28ADDRESS%3D%28PROTOCOL%3DTCP%29%28HOST%3Ddb.example.com%29%28PORT%3D1521%29%29%28CONNECT_DATA%3D%28SERVICE_NAME%3DORCLPDB%29%29%29
```

When a descriptor is used, the listeners and the service name or SID come from the descriptor, so the URI must not also contain a host, port, path, `service_name`, `SID`, or `server`. TNS aliases from `tnsnames.ora` are not supported; use the full descriptor they resolve to.

### Oracle Exadata destination

Use `exadata` when you want to identify the destination explicitly as Exadata:

```plaintext
exadata://user:password@scan-host:1521/service_name
```

`oracle+exadata` and `oracle_exadata` are accepted as aliases. Existing `oracle` and `oracle+cx_oracle` destination URIs work with Exadata as well.

Exadata RAC deployments can pass additional listeners with repeated `server` query parameters. TCPS deployments can pass the thin driver's `SSL`, `SSL VERIFY`, `WALLET`, and `WALLET PASSWORD` options. Query parameter names and values must be URL-encoded.

```plaintext
exadata://user:password@scan1.example.com:1521/analytics?server=scan2.example.com%3A1521&SSL=true&WALLET=%2Fsecure%2Foracle-wallet
```

## Destination notes

Oracle `replace` loads into a staging table first and then finalizes with `ALTER TABLE ... RENAME`. Oracle commits DDL implicitly, so this finalization is not transactional. If finalization fails after the existing target is renamed, ingestr keeps a backup table name in the error message so you can restore it manually with `ALTER TABLE <backup> RENAME TO <target>`.

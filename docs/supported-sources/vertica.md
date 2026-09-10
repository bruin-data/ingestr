# Vertica

[Vertica](https://www.vertica.com/) is a columnar, massively parallel processing (MPP) analytical database built for large-scale analytics, available on-premises, in the cloud, and as a free Community Edition.

ingestr supports Vertica as both a source and a destination.

## URI format
The URI format for Vertica is as follows:

```plaintext
vertica://<username>:<password>@<host>:<port>/<database>
```

URI parameters:
- `username`: the Vertica user (required)
- `password`: the password for the user
- `host`: the Vertica hostname or IP address
- `port`: the Vertica port (default: `5433`)
- `database`: the database to write into

The following optional connection settings can be passed as query parameters:

- `tlsmode`: TLS behavior — `none` (default, plaintext), `prefer` (TLS if the server supports it), `server` (TLS with CA verification), or `server-strict` (also verifies the hostname). Use `tlsmode=server` or `tlsmode=server-strict` over untrusted networks.
- `autocommit`: `1` to enable (default) or `0` to disable autocommit.
- `use_prepared_statements`: `1` to use server-side prepared statements (default) or `0` to disable them.
- `connection_load_balance`: `1` to let the server redirect the connection across nodes for load balancing (off by default).
- `backup_server_node`: a comma-separated list of `host:port` pairs to fail over to if the primary host is unreachable.

```plaintext
vertica://user:pass@host:5433/analytics?tlsmode=server-strict&connection_load_balance=1
```

## Table naming
Vertica organizes tables as `schema.table`. You can specify the destination table in either of these forms:

```plaintext
table            # created in the connection's current schema (usually "public")
schema.table     # fully qualified
```

The schema is created automatically if it does not already exist.

## Setting Vertica as a source

Use the `--source-uri` and `--source-table` flags to read data from Vertica:

```bash
ingestr ingest \
    --source-uri 'vertica://dbadmin:pass@localhost:5433/VMart' \
    --source-table 'public.users' \
    --dest-uri 'postgres://user:pass@localhost:5432/analytics' \
    --dest-table 'public.users'
```

You can also provide a custom query with the `query:` prefix as the source table:

```bash
ingestr ingest \
    --source-uri 'vertica://dbadmin:pass@localhost:5433/VMart' \
    --source-table 'query:SELECT id, email FROM public.users WHERE active = true' \
    --dest-uri 'postgres://user:pass@localhost:5432/analytics' \
    --dest-table 'public.active_users'
```

## Setting Vertica as a destination

Use the `--dest-uri` and `--dest-table` flags to load data into Vertica:

```bash
ingestr ingest \
    --source-uri 'postgres://user:pass@localhost:5432/analytics' \
    --source-table 'public.users' \
    --dest-uri 'vertica://dbadmin:pass@localhost:5433/VMart' \
    --dest-table 'public.users'
```

## Supported strategies
Vertica supports the following load strategies:

- `replace` (default): stages the data and atomically swaps it into the target table
- `append`: inserts the incoming rows into the target table
- `merge`: upserts rows by primary key using a `MERGE` statement
- `delete+insert`: replaces rows in the incremental window and inserts the new rows

Primary keys are required for the `merge` strategy.

## Testing locally

Vertica offers a free Community Edition Docker image you can use to try ingestr:

```bash
docker run -d -p 5433:5433 -p 5444:5444 --name vertica-ce vertica/vertica-ce:12.0.4-0
```

The Community Edition starts with the `VMart` database and the `dbadmin` user (no password), so the destination URI is:

```plaintext
vertica://dbadmin@localhost:5433/VMart
```

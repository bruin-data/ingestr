# Trino
Trino (formerly PrestoSQL) is a distributed SQL query engine designed for fast analytics on large datasets across multiple data sources.

ingestr supports Trino as both a source and destination.

> [!WARNING]
> Trino is currently supported as a beta platform, which means that some features might not work as expected.

## URI format
The URI format for Trino is as follows:

```plaintext
trino://<username>:<password>@<host>:<port>/<catalog>/<schema>
```

URI parameters:
- `username`: your Trino username (required)
- `password`: your Trino password (optional, depending on authentication)
- `host`: the Trino server hostname or IP address
- `port`: the Trino server port (default: 8080)
- `catalog`: the Trino catalog to connect to
- `schema`: the default schema (default: `default`)

Destination URI parameters:

- `json_type`: controls how ingestr writes JSON columns. `varchar` is the default and works across Trino catalogs. `variant` writes Iceberg v3 `VARIANT` columns and requires Trino 481 or newer.

The same URI structure can be used both for sources and destinations. You can read more about SQLAlchemy's Trino dialect [here](https://github.com/trinodb/trino-python-client).

### Authentication methods
Trino supports various authentication methods:

1. **No Authentication**: For development/testing environments
   ```
   trino://user@localhost:8080/catalog
   ```

2. **Basic Authentication**: Username and password
   ```
   trino://user:password@localhost:8080/catalog
   ```

3. **Other Methods**: For Kerberos, JWT, or certificate-based authentication, consult your Trino administrator for the appropriate connection parameters.

## Table naming
When specifying tables for Trino (both source and destination), use the format:

```plaintext
schema.table_name
```

For example:
- `default.users` - accesses the `users` table in the `default` schema
- `analytics.events` - accesses the `events` table in the `analytics` schema

The catalog is specified in the connection URI, not in the table name.

## Examples

### Using Trino as a source
```bash
ingestr ingest \
    --source-uri 'trino://admin@localhost:8080/iceberg' \
    --source-table 'default.source_table' \
    --dest-uri 'duckdb:///output.db' \
    --dest-table 'main.destination_table'
```

### Using Trino as a destination
```bash
ingestr ingest \
    --source-uri 'postgresql://user:pass@localhost:5432/sourcedb' \
    --source-table 'public.customers' \
    --dest-uri 'trino://admin@localhost:8080/hive' \
    --dest-table 'default.customers'
```

### With authentication
```bash
ingestr ingest \
    --source-uri 'mysql://user:pass@localhost:3306/sourcedb' \
    --source-table 'orders' \
    --dest-uri 'trino://user:password@trino-server:8443/iceberg' \
    --dest-table 'sales.orders'
```

### Iceberg v3 variant columns

Use `json_type=variant` to preserve JSON values as native Iceberg v3 semi-structured data:

```bash
ingestr ingest \
    --source-uri 'postgresql://user:pass@localhost:5432/sourcedb' \
    --source-table 'public.events' \
    --dest-uri 'trino://admin@localhost:8080/iceberg/sales?json_type=variant' \
    --dest-table 'sales.events'
```

Ingestr creates new tables with `WITH (format_version = 3)` and writes JSON values as `VARIANT`. Existing tables must already use Iceberg format version 3; ingestr does not upgrade them implicitly.

## Supported write dispositions
When using Trino as a destination, ingestr supports `replace`, `append`, `merge`, and `scd2`.

`delete+insert` is not supported for Trino destinations. Transaction support in Trino depends on the catalog connector, and ingestr does not expose a destination transaction for Trino. Because ingestr cannot make the delete and insert steps atomic across Trino catalogs, it fails this strategy before loading staging data.

## Data type handling
Trino automatically handles most SQL data type conversions. When used as a destination:
- JSON types are converted to `VARCHAR` by default, or to Iceberg v3 `VARIANT` with `json_type=variant`
- Binary types are converted to TEXT/VARCHAR
- All integer types are mapped to BIGINT for compatibility

## Limitations

### As a destination
- Case-sensitive identifiers (table and column names preserve case)
- `VARIANT` JSON columns require Iceberg format version 3 and Trino 481 or newer
- Memory catalog does not support DELETE and UPDATE operations (affects merge/scd2 in test environments)

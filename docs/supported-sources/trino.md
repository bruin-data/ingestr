# Trino
Trino (formerly PrestoSQL) is a distributed SQL query engine designed for fast analytics on large datasets across multiple data sources.

ingestr supports Trino as both a source and destination.

> [!WARNING]
> Trino is currently supported as a beta platform, which means that some features might not work as expected.

## URI format
The URI format for Trino is as follows:

```plaintext
trino://<username>:<password>@<host>:<port>/<catalog>
```

URI parameters:
- `username`: your Trino username (required)
- `password`: your Trino password (optional, depending on authentication)
- `host`: the Trino server hostname or IP address
- `port`: the Trino server port (default: 8080)
- `catalog`: the Trino catalog to connect to

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

### Optional URI query parameters
Additional connection options can be passed as query parameters appended to the URI. These are forwarded to the underlying SQLAlchemy Trino dialect.

**String parameters** (passed as plain URL-encoded strings):

| Parameter | Description |
| --- | --- |
| `access_token` | JWT access token for authentication |
| `cert` | Path to a TLS client certificate (used together with `key`) |
| `key` | Path to a TLS client private key (used together with `cert`) |
| `source` | Client source name reported to Trino (default: `trino-sqlalchemy`) |

**Flag parameter** (any value enables it; the parameter just needs to be present):

| Parameter | Description |
| --- | --- |
| `externalAuthentication` | Enables OAuth2 external authentication |

**JSON parameters** (value must be URL-encoded JSON):

| Parameter | Description | Example value (before URL-encoding) |
| --- | --- | --- |
| `session_properties` | Session properties | `{"query_max_run_time":"1h"}` |
| `http_headers` | Extra HTTP headers | `{"X-Custom":"abc"}` |
| `extra_credential` | List of extra credential tuples | `[["user","alice"]]` |
| `client_tags` | List of client tags | `["etl","prod"]` |
| `legacy_primitive_types` | Use legacy primitive types | `true` |
| `legacy_prepared_statements` | Use legacy prepared statements | `true` |
| `verify` | TLS certificate verification: `true`, `false`, or a JSON-quoted path to a CA bundle (`"/path/to/ca.pem"`) | `true` |
| `roles` | Mapping of catalog → role | `{"hive":"admin"}` |

Examples:

```bash
# JWT authentication
ingestr ingest \
    --source-uri 'trino://user@trino-server:443/iceberg?access_token=eyJhbGciOi...' \
    --source-table 'default.events' \
    --dest-uri 'duckdb:///output.db' \
    --dest-table 'main.events'

# Session properties (URL-encoded JSON)
ingestr ingest \
    --source-uri 'trino://user@host:443/iceberg?session_properties=%7B%22query_max_run_time%22%3A%221h%22%7D' \
    --source-table 'default.events' \
    --dest-uri 'duckdb:///output.db' \
    --dest-table 'main.events'
```

> [!NOTE]
> Trino's HTTP client speaks plain HTTP by default and only auto-switches to HTTPS when the port is `443`. To connect to an HTTPS Trino server on a non-standard port, use port `443` in the URI.

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

## Supported write dispositions
When using Trino as a destination, all the existing write dispositions are supported.

## Data type handling
Trino automatically handles most SQL data type conversions. When used as a destination:
- JSON types are converted to TEXT/VARCHAR
- Binary types are converted to TEXT/VARCHAR
- All integer types are mapped to BIGINT for compatibility

## Limitations

### As a destination
- Case-sensitive identifiers (table and column names preserve case)
- JSON and Binary types are converted to STRING
- Memory catalog does not support DELETE and UPDATE operations (affects merge/scd2 in test environments)
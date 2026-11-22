# CouchDB

[Apache CouchDB](https://couchdb.apache.org/) is an open-source document database that stores data as JSON.

ingestr supports CouchDB as a source.

## URI format

### Standard format (without SSL)

```plaintext
couchdb://username:password@host:port
```

### With SSL/TLS enabled

```plaintext
couchdb+https://username:password@host:port
```

URI parameters:
- `username`: the username to connect to CouchDB, optional for databases that permit anonymous reads
- `password`: the password for the user
- `host`: the host address of the CouchDB server
- `port`: the port number the server is listening on, default is `5984` for HTTP and `443` for HTTPS

> [!IMPORTANT]
> Passwords containing special characters such as `@`, `/`, `#`, or `?` must be URL-encoded in the connection URI.

## Source table format

The `--source-table` option specifies the database to read:

```plaintext
database_name
```

> [!NOTE]
> Do not include the database name in the URI. Use `--source-table` instead.

## Using CouchDB as a source

```bash
ingestr ingest \
  --source-uri "couchdb://admin:password@localhost:5984" \
  --source-table "orders" \
  --dest-uri "duckdb:///warehouse.duckdb" \
  --dest-table "main.orders"
```

This command reads documents from the `orders` database in CouchDB and loads them into the `main.orders` table in DuckDB.

For a server using HTTPS:

```bash
ingestr ingest \
  --source-uri "couchdb+https://username:password@couchdb.example.com" \
  --source-table "orders" \
  --dest-uri "duckdb:///warehouse.duckdb" \
  --dest-table "main.orders"
```

ingestr infers columns from the documents, including `_id` and `_rev`. Nested objects and arrays are stored as JSON. The default primary key is `_id`.

> [!TIP]
> The default `replace` strategy replaces the destination with the current documents. Use `merge` to update existing rows by primary key without removing rows deleted from CouchDB.

## Limitations

- Incremental keys, time intervals, and change data capture (CDC) are not supported. Each run scans the database; `--sql-limit` can limit the number of documents loaded.
- Design documents, local documents, and deleted documents are not ingested. Attachment metadata is included, but attachment contents are not downloaded.
- Reads are not a consistent snapshot if documents change during ingestion.
- Replacing from an empty database clears the destination and creates only `_id` and any custom primary-key columns as strings, since there are no documents from which to infer other columns.

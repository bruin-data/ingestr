# CouchDB

ingestr reads documents from a CouchDB database. Use the database name as `--source-table`.

```sh
ingestr ingest \
  --source-uri 'couchdb://username:password@localhost:5984' \
  --source-table orders \
  --dest-uri 'duckdb:///warehouse.duckdb' \
  --dest-table orders
```

Use `couchdb+https://username:password@host` for TLS (port 443 by default).
The `couchdb://` scheme uses HTTP and defaults to port 5984. Credentials are optional
for databases that permit anonymous reads. URL-encode special characters in credentials.
The URI contains the server address, not the database name.

Document fields become inferred columns, including `_id` and `_rev`. Nested objects
and arrays remain JSON. `_id` is the default primary key. Attachment metadata is
preserved, but attachment bodies are not downloaded. Design documents, local
documents, and deleted documents are not ingested.

The default strategy is `replace`, reading all current documents. An explicit
`merge` upserts by `_id`, but does not remove destination rows deleted from CouchDB.
There is no timestamp filtering or `_changes`/CDC support. Incremental keys and
intervals are rejected rather than silently ignored. `--sql-limit` limits emitted documents.
An empty replacement clears the destination using a minimal `_id` schema, since
there are no documents from which to infer other columns. Custom primary-key
columns are also included as strings in this empty fallback schema.
Pagination is sequential and is not a transactionally consistent snapshot when
documents change during ingestion. Self-hosted CouchDB has no fixed vendor API
quota; the client retries throttling and server errors without imposing a SaaS rate limit.

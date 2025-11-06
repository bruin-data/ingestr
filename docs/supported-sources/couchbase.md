# Couchbase

[Couchbase](https://www.couchbase.com/) is a distributed NoSQL cloud database that delivers unmatched performance, scalability, and flexibility for building modern applications.

ingestr supports Couchbase as a source.

## URI format

### Standard format (without SSL)
```plaintext
couchbase://username:password@host
```

### With SSL/TLS enabled
```plaintext
couchbase://username:password@host?ssl=true
```

### Including bucket in URI
```plaintext
couchbase://username:password@host/bucket
couchbase://username:password@host/bucket?ssl=true
```

URI parameters:
- `username`: the username to connect to the Couchbase cluster
- `password`: the password for the user
- `host`: the host address of the Couchbase server
- `bucket`: optional bucket name in the URI path
- `ssl`: SSL/TLS connection parameter
  - `ssl=true`: Required for Couchbase Capella (cloud) deployments
  - `ssl=false` or omitted: Use for Couchbase Server (self-hosted/on-premises) deployments

> [!NOTE]
> **SSL Parameter Usage:**
> - Use `ssl=true` when connecting to **Couchbase Capella (cloud)**
> - Use `ssl=false` or omit the parameter when connecting to **Couchbase Server (self-hosted/on-premises)**

The URI structure can be used for connecting to both local/self-hosted Couchbase instances and Couchbase Capella (cloud).

## Source table format

The `--source-table` option for Couchbase supports two formats depending on whether the bucket is specified in the URI:

### When bucket is NOT in URI
```plaintext
bucket.scope.collection
```

### When bucket IS in URI path
```plaintext
scope.collection
```

For default scope and collection, you can use:
```plaintext
bucket._default._default
```

## Using Couchbase as a source

### Local/self-hosted Couchbase

#### Basic connection without SSL
```bash
ingestr ingest \
  --source-uri "couchbase://admin:password123@localhost" \
  --source-table "mybucket.myscope.mycollection" \
  --dest-uri "duckdb:///output.db" \
  --dest-table "main.couchbase_data"
```

#### For Couchbase Capella (Cloud)
```bash
ingestr ingest \
  --source-uri "couchbase://admin:password123@localhost?ssl=true" \
  --source-table "mybucket._default._default" \
  --dest-uri "duckdb:///output.db" \
  --dest-table "main.couchbase_data"
```

#### With bucket in URI
```bash
ingestr ingest \
  --source-uri "couchbase://admin:password123@localhost/mybucket" \
  --source-table "myscope.mycollection" \
  --dest-uri "duckdb:///output.db" \
  --dest-table "main.couchbase_data"
```

### Couchbase Capella (Cloud)

> [!IMPORTANT]
> Couchbase Capella (cloud) **requires SSL connections**. You must use `?ssl=true` in your connection URI and prefix the host with `cb.`

> [!TIP]
> You can obtain the connection string for Capella from the SDK connection details in your Couchbase Capella dashboard.

Use the `couchbase://` scheme with `ssl=true` parameter. Note the `cb.` prefix in the hostname:

```bash
ingestr ingest \
  --source-uri "couchbase://username:password@cb.xxx.cloud.couchbase.com?ssl=true" \
  --source-table "travel-sample.inventory.airport" \
  --dest-uri "duckdb:///airports.db" \
  --dest-table "main.airports"
```

With bucket in URI for Couchbase Capella

```bash
ingestr ingest \
  --source-uri "couchbase://username:password@cb.xxx.cloud.couchbase.com/travel-sample?ssl=true" \
  --source-table "inventory.airport" \
  --dest-uri "duckdb:///airports.db" \
  --dest-table "main.airports"
```


### With URL-encoded password

> [!IMPORTANT]
> When using ingestr CLI, passwords containing special characters (`@`, `:`, `/`, `#`, `?`, etc.) **must be URL-encoded** in the connection URI.

If your password contains special characters, you need to URL-encode them:

```bash
ingestr ingest \
  --source-uri "couchbase://admin:MyPass%40123%21@localhost" \
  --source-table "mybucket.myscope.mycollection" \
  --dest-uri "duckdb:///output.db" \
  --dest-table "main.couchbase_data"
```

This example encodes the password `MyPass@123!` as `MyPass%40123%21`.


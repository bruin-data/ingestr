# Elasticsearch
[Elasticsearch](https://www.elastic.co/elasticsearch) is a distributed, RESTful search and analytics engine designed for fast and scalable data retrieval.

ingestr supports Elasticsearch as a source.

## URI format
The URI format for Elasticsearch is as follows:

```plaintext
elasticsearch://username:password@host:port?secure=<secure>&verify_certs=<verify_certs>
```

URI parameters:
- `username`: The username used to authenticate with Elasticsearch.
- `password`: The password associated with the specified username.
- `host`: The host address of the Elasticsearch server.
- `port`: The port number used by the Elasticsearch server.
-  `secure`: Enables HTTPS when set to true. By default, it is true.
- `verify_certs`: Verifies TLS certificates when set to true. By default, it is true.

Source Table

`<index-name>`: Fetches all available documents from the specified index.

```ingestr ingest \
    --source-uri "elasticsearch://elastic:changeme@localhost:9200?secure=false&verify_certs=false" \
    --source-table 'test-index' \
    --dest-uri "duckdb:///users.duckdb" \
    --dest-table 'dest.users_detail'
```

This command retrieves all documents from the test-index in Elasticsearch and copy them to the dest.users_detail table in DuckDB.

<img alt="ingestion_elasticsearch_img" src="../media/ingestion_elasticsearch_img.png" />


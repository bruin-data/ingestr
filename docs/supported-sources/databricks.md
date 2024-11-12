# Databricks
Databricks is a platform for big data analytics and artificial intelligence.

ingestr supports Databricks as both a source and destination.

## URI format
The URI format for Databricks is as follows:

```plaintext
databricks://token:<access_token>@<server_hostname>?http_path=<http_path>&catalog=<catalog>&schema=<schema>
```

URI parameters:
- `access_token`: the access token to connect to the Databricks instance
- `server_hostname`: the hostname of the Databricks instance
- `http_path`: the path to the Databricks instance
- `catalog`: the catalog to connect to
- `schema`: the schema to connect to

The same URI structure can be used both for sources and destinations. You can read more about SQLAlchemy's Databricks dialect [here](https://docs.databricks.com/en/dev-tools/sqlalchemy.html).

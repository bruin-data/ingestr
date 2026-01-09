# Databricks
Databricks is a platform for big data analytics and artificial intelligence.

ingestr supports Databricks as both a source and destination.

## URI format

### Access Token Authentication
The traditional URI format for Databricks using an access token:

```plaintext
databricks://token:<access_token>@<server_hostname>?http_path=<http_path>&catalog=<catalog>&schema=<schema>
```

URI parameters:
- `access_token`: the access token to connect to the Databricks instance
- `server_hostname`: the hostname of the Databricks instance
- `http_path`: the path to the Databricks instance
- `catalog`: the catalog to connect to
- `schema`: the schema to connect to

### OAuth M2M Authentication (Service Principal)
You can also authenticate using OAuth machine-to-machine (M2M) credentials with a service principal's client ID and client secret:

```plaintext
databricks://@<server_hostname>?http_path=<http_path>&catalog=<catalog>&schema=<schema>&client_id=<client_id>&client_secret=<client_secret>
```

URI parameters:
- `server_hostname`: the hostname of the Databricks instance
- `http_path`: the path to the Databricks instance
- `catalog`: the catalog to connect to
- `schema`: the schema to connect to
- `client_id`: the service principal's client ID (application ID)
- `client_secret`: the OAuth secret for the service principal

To set up OAuth M2M authentication:
1. Create a service principal in your Databricks workspace
2. Generate an OAuth secret for the service principal
3. Ensure the service principal has the necessary permissions to access your workspace resources

You can read more about Databricks OAuth M2M authentication [here](https://docs.databricks.com/en/dev-tools/auth/oauth-m2m.html).

The same URI structure can be used both for sources and destinations. You can read more about SQLAlchemy's Databricks dialect [here](https://docs.databricks.com/en/dev-tools/sqlalchemy.html).

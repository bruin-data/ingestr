# Snowflake
Snowflake is a cloud-based data warehousing platform that supports structured and semi-structured data.

ingestr supports Snowflake as both a source and destination.

## URI format
The URI format for Snowflake is as follows:

```plaintext
snowflake://user:password@account/dbname?warehouse=COMPUTE_WH&role=data_scientist
```

URI parameters:
- `user`: the user name to connect to the database
- `password`: the password for the user
- `account`: your Snowflake account identifier (copying from snowflake interface gives you org_name.account_name, modify the "." to "-" in the ingestr command)
- `dbname`: the name of the database to connect to
- `warehouse`: optional, the name of the warehouse to use
- `role`: optional, the name of the role to use

The same URI structure can be used both for sources and destinations. You can read more about SQLAlchemy's Snowflake dialect [here](https://docs.snowflake.com/en/developer-guide/python-connector/sqlalchemy#connection-parameters).

## Key-Pair Authentication

Snowflake supports key-pair (JWT) authentication as an alternative to password-based authentication. To use it, pass the private key as a URI parameter instead of a password:

```plaintext
snowflake://user@account/dbname?warehouse=COMPUTE_WH&role=data_scientist&private_key=<url-encoded-pem-key>
```

You can URL-encode the private key inline or read it from a file:

```bash
ingestr ingest \
  --source-uri="snowflake://USER@account/dbname?warehouse=WH&role=ROLE&private_key=$(python3 -c "import urllib.parse; print(urllib.parse.quote(open('path/to/private_key.pem').read().strip()))")" \
  --source-table="schema.table_name" \
  --dest-uri="duckdb:///path/to/output.duckdb" \
  --dest-table="main.table_name"
```

### Setting up key-pair authentication

#### Step 1: Generate a key pair

```bash
openssl genrsa 2048 | openssl pkcs8 -topk8 -inform PEM -out rsa_key.p8 -nocrypt
openssl rsa -in rsa_key.p8 -pubout -out rsa_key.pub
```

#### Step 2: Assign the public key to your Snowflake user

Log into Snowflake and run:

```sql
ALTER USER your_username SET RSA_PUBLIC_KEY='<contents of rsa_key.pub without header/footer>';
```

#### Step 3: Use the private key in the URI

Pass the private key file via the `private_key` query parameter as shown above.

If your private key is encrypted with a passphrase, add the `private_key_passphrase` parameter:

```plaintext
snowflake://user@account/dbname?private_key=<key>&private_key_passphrase=<passphrase>
```

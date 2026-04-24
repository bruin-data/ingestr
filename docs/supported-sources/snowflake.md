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

Snowflake supports key-pair (JWT) authentication as an alternative to password-based authentication. To use it, pass the private key via the `private_key` query parameter instead of a password:

```plaintext
snowflake://user@account/dbname?warehouse=COMPUTE_WH&role=data_scientist&private_key=<private-key>
```

The private key can be provided in several formats:

### Option 1: URL-encoded PEM file

Read the PEM file and URL-encode it:

```bash
ingestr ingest \
  --source-uri="snowflake://USER@account/dbname?warehouse=WH&role=ROLE&private_key=$(python3 -c "import urllib.parse; print(urllib.parse.quote(open('path/to/private_key.pem').read().strip()))")" \
  --source-table="schema.table_name" \
  --dest-uri="duckdb:///path/to/output.duckdb" \
  --dest-table="main.table_name"
```

### Option 2: Base64-encoded DER

Convert the PEM key to base64 DER (a single line with no headers), which avoids URL-encoding issues:

```bash
# Convert PEM to base64 DER
KEY=$(openssl pkey -in private_key.pem -outform DER | base64 | tr -d '\n')

ingestr ingest \
  --source-uri="snowflake://USER@account/dbname?warehouse=WH&role=ROLE&private_key=$KEY" \
  --source-table="schema.table_name" \
  --dest-uri="duckdb:///path/to/output.duckdb" \
  --dest-table="main.table_name"
```

### Option 3: Raw PEM content

The raw PEM content (including `-----BEGIN PRIVATE KEY-----` headers) can be passed directly, but it must be URL-encoded due to newlines and special characters in the PEM format. See Option 1 for how to do this.

If your private key is encrypted with a passphrase, add the `private_key_passphrase` parameter:

```plaintext
snowflake://user@account/dbname?private_key=<key>&private_key_passphrase=<passphrase>
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

Pass the private key via the `private_key` query parameter using any of the options above.

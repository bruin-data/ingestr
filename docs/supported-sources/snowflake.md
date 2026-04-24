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

If your private key is encrypted with a passphrase, add the `private_key_passphrase` parameter:

```plaintext
snowflake://user@account/dbname?private_key=<key>&private_key_passphrase=<passphrase>
```

### Setting up key-pair authentication

#### Step 1: Generate a key pair

Open your terminal and run the following command to create a key pair. If you're using a mac, OpenSSL should be installed by default, so no additional setup is required. For Linux or Windows, you may need to [install OpenSSL first](https://docs.openssl.org/3.4/man7/ossl-guide-introduction/).

```bash
openssl genrsa 2048 | openssl pkcs8 -topk8 -inform PEM -out rsa_key.p8 -nocrypt
openssl rsa -in rsa_key.p8 -pubout -out rsa_key.pub
```

#### Step 2: Set public key for Snowflake user

Log into Snowflake as an admin, create a new worksheet and run the following command (don't forget the single quotes around the key):

```sql
ALTER USER your_snowflake_username
SET RSA_PUBLIC_KEY='your_public_key_here';
```

#### Step 3: Verify

```sql
DESC USER your_snowflake_username;
```

This will show a column named `RSA_PUBLIC_KEY`. You should see your actual key there.

#### Step 4: Use the private key in the URI

Convert the key to base64 DER format and pass it as the `private_key` parameter:

```bash
# Convert PEM to base64 DER (single line, no headers)
openssl pkey -in rsa_key.p8 -outform DER | base64

# Use the output as the private_key parameter
ingestr ingest \
  --source-uri="snowflake://USER@account/dbname?warehouse=WH&role=ROLE&private_key=<base64-der-key>" \
  --source-table="schema.table_name" \
  --dest-uri="duckdb:///path/to/output.duckdb" \
  --dest-table="main.table_name"
```

For more details on how to set up key-based authentication, see [this guide](https://select.dev/docs/snowflake-developer-guide/snowflake-key-pair).

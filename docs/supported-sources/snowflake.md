# Snowflake
Snowflake is a cloud-based data warehousing platform that supports structured and semi-structured data.

ingestr supports Snowflake as both a source and destination.

## URI Format
The URI format for Snowflake is as follows:

```plaintext
snowflake://user:password@account/dbname?warehouse=COMPUTE_WH
```

URI parameters:
- `user`: the user name to connect to the database
- `password`: the password for the user
- `account`: your Snowflake account identifier
- `dbname`: the name of the database to connect to
- `warehouse`: the name of the warehouse to use

The same URI structure can be used both for sources and destinations. You can read more about SQLAlchemy's Snowflake dialect [here](https://docs.snowflake.com/en/developer-guide/python-connector/sqlalchemy#connection-parameters).

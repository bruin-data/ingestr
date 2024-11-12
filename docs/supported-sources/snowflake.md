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

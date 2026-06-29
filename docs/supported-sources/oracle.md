# Oracle
Oracle is a powerful, fully integrated stack of cloud applications and platform services, known for its comprehensive capabilities in database management.

ingestr supports Oracle as a source and destination through the `oracle+cx_oracle`-compatible URI format. Under the hood, ingestr uses the Go Oracle driver in thin mode, which means **no Oracle Client libraries are required** to be installed on your system.

## URI format
The URI format for Oracle is as follows:

```plaintext
oracle+cx_oracle://user:password@host:port/dbname
```

URI parameters:
- `user`: the user name to connect to the database
- `password`: the password for the user
- `host`: the host address of the database server
- `port`: the port number the database server is listening on, the default is 1521 for Oracle databases
- `dbname`: the name of the database

The same URI structure can be used both for sources and destinations. You can read more about SQLAlchemy's Oracle dialect [here](https://docs.sqlalchemy.org/en/20/core/engines.html#oracle).

## Destination notes

Oracle `replace` loads into a staging table first and then finalizes with `ALTER TABLE ... RENAME`. Oracle commits DDL implicitly, so this finalization is not transactional. If finalization fails after the existing target is renamed, ingestr keeps a backup table name in the error message so you can restore it manually with `ALTER TABLE <backup> RENAME TO <target>`.

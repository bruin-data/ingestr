# Oracle
Oracle is a powerful, fully integrated stack of cloud applications and platform services, known for its comprehensive capabilities in database management.

ingestr supports Oracle as an experimental source through `oracle+cx_oracle` driver.

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

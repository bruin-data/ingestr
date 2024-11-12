# Postgres
Postgres is an open source, object-relational database system that provides reliability, data integrity, and correctness.

ingestr supports Postgres as both a source and destination.

## URI format
The URI format for Postgres is as follows:

```plaintext
postgresql://<username>:<password>@<host>:<port>/<database-name>?sslmode=<sslmode>
```

URI parameters:
- `username`: the user name to connect to the database
- `password`: the password for the user
- `host`: the host address of the database server
- `port`: the port number the database server is listening on, the default is 5432
- `database-name`: the name of the database to connect to
- `sslmode`: optional, the SSL mode to use when connecting to the database

The same URI structure can be used both for sources and destinations. You can read more about SQLAlchemy's Postgres dialect [here](https://docs.sqlalchemy.org/en/14/dialects/postgresql.html).

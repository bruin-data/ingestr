# SQLite
SQLite is a C-language library that implements a small, fast, self-contained, high-reliability, full-featured, SQL database engine.

ingestr supports SQLite as a source.

## URI format
The URI format for SQLite is as follows:

```plaintext
sqlite:///<database-file>
```

URI parameters:
- `database-file`: the path to the SQLite database file.

The same URI structure can be used both for sources and destinations. You can read more about SQLAlchemy's SQLite dialect [here](https://docs.sqlalchemy.org/en/20/core/engines.html#sqlite).

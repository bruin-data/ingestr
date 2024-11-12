# DuckDB
DuckDB is an in-memory database designed to be fast and reliable.

ingestr supports DuckDB as both a source and destination.

## URI format
The URI format for DuckDB is as follows:

```plaintext
duckdb:///<database-file>
```

URI parameters:
- `database-file`: the path to the DuckDB database file

The same URI structure can be used both for sources and destinations. You can read more about SQLAlchemy's DuckDB dialect [here](https://github.com/Mause/duckdb_engine).

---
outline: deep
---

# Quickstart
ingestr is a command-line app that allows you to ingest data from any source into any destination using simple command-line flags, no code necessary.

- ✨ copy data from your Postgres / Mongo / BigQuery or any other source into any destination
- ➕ incremental loading: `append`, `merge` or `delete+insert`
- 🐍 single-command installation

ingestr takes away the complexity of managing any backend or writing any code for ingesting data, simply run the command and watch the magic.


## Installation

You can install `ingestr` using the install script:

```sh
curl -LsSf https://getbruin.com/install/ingestr | sh
````

Alternatively, you can install it with pip:

```sh
pip install ingestr
```


## Quickstart

```bash
ingestr ingest \
    --source-uri 'chess://?players=awryaw,albertojgomez' \
    --source-table 'profiles' \
    --dest-uri 'duckdb:///./chess.duckdb' \
    --dest-table 'raw.profiles'
```

___That's it.___

> [!INFO]
> The steps here assume you have [DuckDB](https://duckdb.org/install/) installed. DuckDB runs locally with zero setup and keeps the quickstart easy and fast.

This command will:
- fetch the `profiles` table for the requested chess players.
- write the data into the DuckDB database at `./chess.duckdb` under `raw.profiles`.

If you'd like a quick check, you can query the table directly:
```bash
duckdb ./chess.duckdb "select * from raw.profiles"
```

Or alternatively explore the table in the DuckDB UI:
```bash
duckdb -ui ./chess.duckdb
```

## Supported sources & destinations

See the Supported Sources & Destinations page for a list of all supported sources and destinations.
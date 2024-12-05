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
We recommend using [uv](https://github.com/astral-sh/uv) to run `ingestr`.

```
pip install uv
uvx ingestr
```

Alternatively, if you'd like to install it globally:
```
uv pip install --system ingestr
```

While installation with vanilla `pip` is possible, it's an order of magnitude slower.

## Quickstart

```bash
ingestr ingest \
    --source-uri 'postgresql://admin:admin@localhost:8837/web?sslmode=disable' \
    --source-table 'public.some_data' \
    --dest-uri 'bigquery://<your-project-name>?credentials_path=/path/to/service/account.json' \
    --dest-table 'ingestr.some_data'
```

That's it.

This command:
- gets the table `public.some_data` from the Postgres instance.
- uploads this data to your BigQuery warehouse under the schema `ingestr` and table `some_data`.


## Supported sources & destinations

See the Supported Sources & Destinations page for a list of all supported sources and destinations.
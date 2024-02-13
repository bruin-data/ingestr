<div align="center">
    <img src="./resources/ingestr.svg" width="500" />
    <p>Ingest & copy data from any source to any destination without any code</p>
</div>

-----

Ingestr is a command-line application that allows you to ingest data from any source into any destination using simple command-line flags, no code necessary.

- ✨ copy data from your Postges / Mongo / BigQuery or any other source into any destination
- ➕ incremental loading
- 🐍 single-command installation
- 💅 Docker image for easy installation & usage

ingestr takes away the complexity of managing any backend or writing any code for ingesting data, simply run the command and watch the magic.


## Installation
```
pip install ingestr
```

## Quickstart

```bash
ingestr \
    --source-uri 'postgresql://admin:admin@localhost:8837/web?sslmode=disable' \
    --source-table 'public.some_data' \
    --dest-uri 'bigquery://<your-project-name>?credentials_path=/path/to/service/account.json' \
    --dest-table 'ingestr.some_data'
```

That's it.

This command will:
- get the table `public.some_data` from the Postgres instance.
- upload this data to your BigQuery warehouse under the schema `ingestr` and table `some_data`.

## Supported Sources & Destinations

| Database             | Source | Destination |
|----------------------|--------|-------------|
| Postgres             | ✅      | ✅         |
| BigQuery             | ✅      | ✅         |
| Snowflake            | ✅      | ✅         |
| Redshift             | ✅      | ✅         |
| Databricks           | ✅      | ✅         |
| DuckDB               | ✅      | ✅         |
| Microsoft SQL Server | ✅      | ✅         |
| SQLite               | ✅      | ❌         |
| MySQL                | ✅      | ❌         |
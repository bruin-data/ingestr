---
outline: deep
---

# Core Concepts
ingestr has a few simple concepts that you should understand before you start using it.

## Source & destination URIs
The source and destination are the two main components of ingestr. The source is the place from where you want to ingest the data, hence the name "source" and the destination is the place where you want to store the data.

The sources and destinations are identified with [URIs](https://en.wikipedia.org/wiki/Uniform_Resource_Identifier). A URI is a simple string that contains the credentials used to connect to the source or destination.

Here's an example URI for a Postgres database:
```
postgresql://admin:admin@localhost:8837/web?sslmode=disable
```

The URI is composed of the following parts:
- `postgresql`: the name of the database
- `admin:admin`: the username and password
- `localhost:8837`: the host and port
- `web`: the database name
- `sslmode=disable`: the query parameters

ingestr can connect to any source or destination using this structure across all databases.

> [!NOTE]
> ingestr uses [dlt](https://github.com/dlt-hub/dlt) & [SQLAlchemy](https://www.sqlalchemy.org/) libraries internally, which means you can get connection URIs by following their documentation as well, they are supposed to work right away in ingestr.

## Source & destination tables
The source and destination tables are the tables from the source and destination databases, respectively. The source table is the table from where you want to ingest the data from, and the destination table is the table where you want to store the data.

ingestr uses the `--source-table` and `--dest-table` flags to specify the source and destination tables, respectively. The `--dest-table` is optional, if you don't specify it, ingestr will use the same table name as the source table.


## Incremental loading
ingestr supports incremental loading, which means you can choose to append, merge or delete+insert data into the destination table. Incremental loading allows you to ingest only the new rows from the source table into the destination table, which means that you don't have to ingest the entire table every time you run ingestr.

Incremental loading requires various identifiers in the source table to understand what has changed when, so that the new rows can be ingested into the destination table. Read more in the [Incremental Loading](/getting-started/incremental-loading.md) section.



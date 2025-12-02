# Bruin

[Bruin](https://getbruin.com/) is a data platform that allows you to build, test, and deploy data pipelines. Bruin Cloud provides an API to access your pipeline metadata and execution information.

ingestr supports Bruin as a source.

## URI format

The URI format for Bruin is as follows:

```plaintext
bruin://?api_key=<api-key-here>
```

URI parameters:

- `api_key`: the API key used for authentication with the Bruin API

## Setting up a Bruin Integration

To get your API key:

1. Go to [cloud.getbruin.com](https://cloud.getbruin.com)
2. Navigate to **Teams** section
3. Click on **Create API Token**
4. Make sure **Pipeline List** is selected as the permission
5. Copy the generated API key

Once you have your API key, here's a sample command that will copy the data from Bruin into a DuckDB database:

```sh
ingestr ingest \
    --source-uri 'bruin://?api_key=your_api_key_here' \
    --source-table 'pipelines' \
    --dest-uri duckdb:///bruin.duckdb \
    --dest-table 'dest.pipelines'
```

## Tables

Bruin source allows ingesting the following sources into separate tables:

| Table | Inc Key | Inc Strategy | Description |
| ----- | ------- | ------------ | ----------- |
| `pipelines` | - | replace | Contains information about your data pipelines including metadata and configuration. |
| `assets` | - | replace | Contains information about your pipeline assets including their definitions and dependencies. |

Use these as `--source-table` parameter in the `ingestr ingest` command.

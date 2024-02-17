# `ingestr ingest`

The `ingest` command is a core functionality of the `ingestr` tool, allowing users to transfer data from a source to a destination with optional support for incremental updates.

## Example

The following example demonstrates how to use the `ingest` command to transfer data from a source to a destination.

```bash
ingestr ingest 
   --source-uri '<your-source-uri-here>'
   --source-table '<your-schema>.<your-table>'
   --dest-uri '<your-destination-uri-here>'
```

## Required Options

- `--source-uri TEXT`: Specifies the URI of the data source. This parameter is required.
- `--dest-uri TEXT`: Specifies the URI of the destination where data will be ingested. This parameter is required.
- `--source-table TEXT`: Defines the source table to fetch data from. This parameter is required.

## Optional Options

- `--dest-table TEXT`: Designates the destination table to save the data. If not specified, defaults to the value of `--source-table`.
- `--incremental-key TEXT`: Identifies the key used for incremental data strategies. Defaults to `None`.
- `--incremental-strategy TEXT`: Defines the strategy for incremental updates. Options include `replace`, `append`, `delete+insert`, or `merge`. The default strategy is `replace`.
- `--interval-start`: Sets the start of the interval for the incremental key. Defaults to `None`.
- `--interval-end`: Sets the end of the interval for the incremental key. Defaults to `None`.
- `--primary-key TEXT`: Specifies the primary key for the merge operation. Defaults to `None`.

The `interval-start` and `interval-end` options support various datetime formats, here are some examples:
- `%Y-%m-%d`: `2023-01-31`
- `%Y-%m-%dT%H:%M:%S`: `2023-01-31T15:00:00`
- `%Y-%m-%dT%H:%M:%S%z`: `2023-01-31T15:00:00+00:00`
- `%Y-%m-%dT%H:%M:%S.%f`: `2023-01-31T15:00:00.000123`
- `%Y-%m-%dT%H:%M:%S.%f%z`: `2023-01-31T15:00:00.000123+00:00`

> [!INFO]
> For the details around the incremental key and the various strategies, please refer to the [Incremental Loading](../getting-started/incremental-loading.md) section.

## General Options

- `--help`: Displays the help message and exits the command.


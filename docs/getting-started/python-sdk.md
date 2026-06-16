---
outline: deep
---

# Python SDK

The `ingestr` pip package can be used as a Python library when your source data already lives in Python: API responses, yielded pages, dictionaries, pandas DataFrames, Polars DataFrames, or PyArrow tables.

The SDK still runs the bundled `ingestr` binary for the actual ingestion work. Python data is passed to the process as Arrow IPC by default, so the Go pipeline can load it into any supported destination.

## Installation

Install `ingestr` with the SDK extra:

```sh
pip install 'ingestr[sdk]'
```

The `sdk` extra installs `pyarrow`, which is required for Python data ingestion.

## Ingest rows

Pass an iterable of mapping-like rows to `ingestr.ingest`:

```python
import ingestr

ingestr.ingest(
    [
        {"id": 1, "name": "Ada"},
        {"id": 2, "name": "Grace"},
    ],
    dest_uri="duckdb:///tmp/warehouse.duckdb",
    dest_table="main.people",
)
```

Rows can be dictionaries, dataclasses, Pydantic models, named tuples, or regular Python objects with attributes.

You can pass ingestion options through the Python API. For example, `trim_whitespace=True` trims leading and trailing whitespace from string values before they are written:

```python
ingestr.ingest(
    [{"id": 1, "name": "  Ada  "}],
    dest_uri="duckdb:///tmp/warehouse.duckdb",
    dest_table="main.people",
    trim_whitespace=True,
)
```

## Ingest yielded data

Generator functions can yield individual rows or pages of rows:

```python
import ingestr

def events():
    yield {"id": 1, "event": "signup"}
    yield [
        {"id": 2, "event": "purchase"},
        {"id": 3, "event": "refund"},
    ]

ingestr.ingest(
    events,
    dest_uri="postgresql://user:pass@localhost:5432/app",
    dest_table="public.events",
)
```

You can also pass an already-created generator:

```python
ingestr.ingest(events(), dest_uri="postgresql://...", dest_table="public.events")
```

If your function needs arguments, pass a wrapper:

```python
def events_for_account(account_id):
    for page in client.list_events(account_id):
        yield page["items"]

ingestr.ingest(
    lambda: events_for_account("acct_123"),
    dest_uri="postgresql://...",
    dest_table="public.events",
)
```

Async generators are not supported yet. Use a synchronous iterator or collect pages from async code before passing them to `ingestr.ingest`.

## Ingest DataFrames and Arrow data

The same `ingestr.ingest` method accepts pandas DataFrames, Polars DataFrames, PyArrow tables, PyArrow record batches, and PyArrow record batch readers:

```python
ingestr.ingest(
    df,
    dest_uri="duckdb:///tmp/warehouse.duckdb",
    dest_table="main.events",
)
```

## Push data with a context manager

For push-style code, omit the data argument and use `ingestr.ingest` as a context manager. The value yielded by the context manager is a callable sink that accepts the same data shapes as `ingestr.ingest(data, ...)`.

```python
with ingestr.ingest(
    dest_uri="postgresql://user:pass@localhost:5432/app",
    dest_table="public.events",
) as ingest:
    for response in client.list_events():
        ingest(response["items"])

result = ingest.result
```

A bare `yield` inside the `with` block yields from your Python function; it does not send data to ingestr. Use the sink function inside the block, or pass a generator function directly to `ingestr.ingest`.

## Transport options

By default, Python data is streamed to the bundled binary over Arrow IPC:

```python
ingestr.ingest(rows, dest_uri="duckdb:///tmp/warehouse.duckdb", dest_table="main.rows")
```

For very large already-materialized DataFrames or Arrow tables, you can use the mmap Arrow IPC file transport:

```python
ingestr.ingest(
    df,
    dest_uri="duckdb:///tmp/warehouse.duckdb",
    dest_table="main.events",
    transport="mmap",
)
```

Use the default stream transport for generators and data produced incrementally. Use `transport="mmap"` when the data is already materialized and you want the binary to read it from an Arrow IPC file.

## CLI passthrough

The Python package also exposes helpers for running the CLI directly:

```python
ingestr.run(["ingest", "--source-uri", "...", "--dest-uri", "...", "--source-table", "..."])
```

Use `run_cli` when you want keyword arguments mapped to `ingestr ingest` flags:

```python
ingestr.run_cli(
    source_uri="csv:///tmp/events.csv",
    source_table="events",
    dest_uri="duckdb:///tmp/warehouse.duckdb",
    dest_table="main.events",
    trim_whitespace=True,
)
```

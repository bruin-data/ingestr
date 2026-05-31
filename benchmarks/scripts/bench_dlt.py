# /// script
# requires-python = ">=3.9"
# dependencies = [
#     "dlt[postgres,duckdb,bigquery,snowflake]==1.27.0",
#     "dlt-verified-sources @ git+https://github.com/dlt-hub/verified-sources.git@75b3ec17eab99d0079d9f61b7f47fc8b899a5738",
#     "pymysql",
#     "pymongo",
#     "sqlalchemy>=1.4,<2",
#     "duckdb-engine",
#     "pyarrow",
#     "numpy",
# ]
# ///
"""Benchmark script for dlt-hub. Run via: uv run bench_dlt.py --source-uri ... --dest-uri ..."""

import argparse
import json
import os
import tempfile

os.environ.setdefault("RUNTIME__LOG_LEVEL", "ERROR")
os.environ.setdefault("RUNTIME__DLTHUB_TELEMETRY", "false")

import dlt
from dlt.sources.sql_database import sql_table
from sources.mongodb import mongodb
from sqlalchemy import Float


def normalize_source_uri(uri: str) -> str:
    if uri.startswith("postgres://"):
        return uri.replace("postgres://", "postgresql://", 1)
    if uri.startswith("mysql://"):
        return uri.replace("mysql://", "mysql+pymysql://", 1)
    return uri


def duckdb_path_from_uri(uri: str) -> str:
    return uri.split("duckdb:///", 1)[1]


def parse_mongodb_table(table: str) -> tuple[str, str, dict | None]:
    collection_part, _, filter_json = table.partition(":")
    if "." not in collection_part:
        raise ValueError(f"MongoDB source table must be database.collection, got: {table}")
    database, collection = collection_part.split(".", 1)
    if not filter_json:
        return database, collection, None

    filter_ = json.loads(filter_json)
    if not isinstance(filter_, dict):
        raise ValueError(
            "The official dlt MongoDB source supports a JSON object filter after "
            "the ':' suffix; aggregation pipelines are not supported."
        )
    return database, collection, filter_


def mongodb_source(uri: str, table: str):
    database, collection, filter_ = parse_mongodb_table(table)
    return mongodb(
        connection_url=uri,
        database=database,
        collection_names=[collection],
        filter_=filter_ or {},
    )


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--source-uri")
    parser.add_argument("--source-uri-env")
    parser.add_argument("--source-table", required=True)
    parser.add_argument("--dest-uri")
    parser.add_argument("--dest-uri-env")
    parser.add_argument("--dest-table", required=True)
    args = parser.parse_args()

    if args.source_uri_env:
        args.source_uri = os.environ.get(args.source_uri_env)
    if args.dest_uri_env:
        args.dest_uri = os.environ.get(args.dest_uri_env)
    if not args.source_uri:
        raise ValueError("--source-uri or --source-uri-env is required")
    if not args.dest_uri:
        raise ValueError("--dest-uri or --dest-uri-env is required")

    if "." in args.source_table:
        src_schema, src_table = args.source_table.split(".", 1)
    else:
        src_schema, src_table = None, args.source_table

    if "." in args.dest_table:
        dest_schema, dest_table = args.dest_table.split(".", 1)
    else:
        dest_schema, dest_table = None, args.dest_table

    source_uri = normalize_source_uri(args.source_uri)

    if source_uri.startswith(("mongodb://", "mongodb+srv://")):
        source = mongodb_source(source_uri, args.source_table)
    else:
        def cast_doubles(table):
            for col in table.columns:
                if str(col.type) == "DOUBLE":
                    col.type = Float()

        source = sql_table(
            credentials=source_uri,
            table=src_table,
            schema=src_schema,
            backend="pyarrow",
            table_adapter_callback=cast_doubles,
        )

    # Build destination
    dest_uri = args.dest_uri
    if dest_uri.startswith(("postgres://", "postgresql://")):
        pg_uri = dest_uri.replace("postgres://", "postgresql://", 1) if dest_uri.startswith("postgres://") else dest_uri
        destination = dlt.destinations.postgres(credentials=pg_uri)
    elif dest_uri.startswith("duckdb://"):
        db_path = duckdb_path_from_uri(dest_uri)
        destination = dlt.destinations.duckdb(credentials=db_path)
    elif dest_uri.startswith("bigquery://"):
        parts = dest_uri.replace("bigquery://", "").split("/", 1)
        dest_schema = parts[1] if len(parts) > 1 else dest_schema
        bq_kwargs = {}
        creds = os.environ.get("GOOGLE_APPLICATION_CREDENTIALS")
        if creds:
            bq_kwargs["credentials"] = creds
        destination = dlt.destinations.bigquery(**bq_kwargs)
    elif dest_uri.startswith("snowflake://"):
        destination = dlt.destinations.snowflake(credentials=dest_uri)
    else:
        raise ValueError(f"Unsupported destination: {dest_uri}")

    pipeline_kwargs = dict(
        pipeline_name="bench_dlt",
        destination=destination,
        dataset_name=dest_schema or "main",
        pipelines_dir=os.path.join(tempfile.mkdtemp(), "dlt_pipelines"),
    )
    pipeline = dlt.pipeline(**pipeline_kwargs)

    run_kwargs = dict(
        table_name=dest_table,
        write_disposition="replace",
    )
    if dest_uri.startswith("bigquery://"):
        run_kwargs["loader_file_format"] = "jsonl"

    info = pipeline.run(source, **run_kwargs)
    print(info)


if __name__ == "__main__":
    main()

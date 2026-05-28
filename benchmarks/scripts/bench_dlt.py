# /// script
# requires-python = ">=3.9"
# dependencies = [
#     "dlt[postgres,duckdb,bigquery]==1.27.0",
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
from bson import Decimal128, ObjectId
from dlt.sources.sql_database import sql_table
from sqlalchemy import Float


def normalize_source_uri(uri: str) -> str:
    if uri.startswith("postgres://"):
        return uri.replace("postgres://", "postgresql://", 1)
    if uri.startswith("mysql://"):
        return uri.replace("mysql://", "mysql+pymysql://", 1)
    return uri


def duckdb_path_from_uri(uri: str) -> str:
    return uri.split("duckdb:///", 1)[1]


def parse_mongodb_table(table: str) -> tuple[str, str, list[dict] | None]:
    collection_part, _, query_json = table.partition(":")
    if "." not in collection_part:
        raise ValueError(f"MongoDB source table must be database.collection, got: {table}")
    database, collection = collection_part.split(".", 1)
    pipeline = json.loads(query_json) if query_json else None
    return database, collection, pipeline


def normalize_bson(value):
    if isinstance(value, ObjectId):
        return str(value)
    if isinstance(value, Decimal128):
        return float(value.to_decimal())
    if isinstance(value, dict):
        return {k: normalize_bson(v) for k, v in value.items()}
    if isinstance(value, list):
        return [normalize_bson(v) for v in value]
    return value


def mongodb_resource(uri: str, table: str):
    from pymongo import MongoClient

    database, collection, pipeline = parse_mongodb_table(table)

    @dlt.resource(name=collection)
    def read_collection():
        client = MongoClient(uri)
        try:
            coll = client[database][collection]
            if pipeline:
                cursor = coll.aggregate(pipeline, allowDiskUse=True)
            else:
                cursor = coll.find({}, no_cursor_timeout=True).batch_size(10000)
            try:
                for doc in cursor:
                    yield normalize_bson(doc)
            finally:
                cursor.close()
        finally:
            client.close()

    return read_collection


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--source-uri", required=True)
    parser.add_argument("--source-table", required=True)
    parser.add_argument("--dest-uri", required=True)
    parser.add_argument("--dest-table", required=True)
    args = parser.parse_args()

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
        source = mongodb_resource(source_uri, args.source_table)
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

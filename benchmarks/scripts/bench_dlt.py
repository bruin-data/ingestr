# /// script
# requires-python = ">=3.12,<3.13"
# dependencies = [
#     "dlt[postgres,duckdb,bigquery,snowflake,mssql]==1.27.2",
#     "dlt-verified-sources @ git+https://github.com/dlt-hub/verified-sources.git@75b3ec17eab99d0079d9f61b7f47fc8b899a5738",
#     "duckdb-engine>=0.17.0",
#     "pendulum>=3.0.0",
#     "pyarrow>=17.0,<17.1",
#     "pymongo>=4.4",
#     "pymongoarrow==1.5.2",
#     "pymysql",
#     "sqlalchemy>=1.4,<3",
# ]
# ///
"""Benchmark script for dlt-hub."""

import argparse
import json
import os
import tempfile
from urllib.parse import parse_qsl, urlencode, urlparse, urlunparse

os.environ.setdefault("RUNTIME__LOG_LEVEL", "ERROR")
os.environ.setdefault("RUNTIME__DLTHUB_TELEMETRY", "false")

import dlt
from dlt.sources.sql_database import sql_table
from sources.mongodb import mongodb, mongodb_collection


def normalize_source_uri(uri: str) -> str:
    if uri.startswith("postgres://"):
        return uri.replace("postgres://", "postgresql://", 1)
    if uri.startswith("mysql://"):
        return uri.replace("mysql://", "mysql+pymysql://", 1)
    if uri.startswith(("mssql://", "sqlserver://")):
        return normalize_mssql_uri(uri)
    return uri


def normalize_mssql_uri(uri: str) -> str:
    parsed = urlparse(uri)
    scheme = "mssql+pyodbc" if parsed.scheme in ("mssql", "sqlserver") else parsed.scheme
    params = dict(parse_qsl(parsed.query, keep_blank_values=True))

    encrypt = params.pop("encrypt", params.pop("Encrypt", None))
    if encrypt is not None:
        params["Encrypt"] = "no" if encrypt.lower() in ("disable", "false", "no", "0") else encrypt

    params.setdefault("driver", "ODBC Driver 18 for SQL Server")
    params.setdefault("TrustServerCertificate", "yes")

    return urlunparse(parsed._replace(scheme=scheme, query=urlencode(params)))


def patch_dlt_mssql_json_type():
    from dlt.destinations.impl.mssql.factory import MsSqlTypeMapper

    MsSqlTypeMapper.sct_to_unbound_dbt = {
        **MsSqlTypeMapper.sct_to_unbound_dbt,
        "json": "nvarchar(max)",
    }


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


def mongodb_source(uri: str, table: str, backend: str):
    database, collection, filter_ = parse_mongodb_table(table)

    if backend == "default":
        return mongodb(
            connection_url=uri,
            database=database,
            collection_names=[collection],
            filter_=filter_ or {},
        )

    # The verified source exposes data_item_format only on the collection-level
    # resource, so the pyarrow variant intentionally uses that lower-level API.
    from dlt.extract.source import DltResource

    mongodb_collection.__wrapped__.__annotations__["return"] = DltResource
    source_kwargs = dict(
        connection_url=uri,
        database=database,
        collection=collection,
        data_item_format="arrow" if backend == "pyarrow" else "object",
    )
    if filter_:
        source_kwargs["filter_"] = filter_
    return mongodb_collection(**source_kwargs)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--source-uri")
    parser.add_argument("--source-uri-env")
    parser.add_argument("--source-table", required=True)
    parser.add_argument("--dest-uri")
    parser.add_argument("--dest-uri-env")
    parser.add_argument("--dest-table", required=True)
    parser.add_argument(
        "--backend",
        choices=("default", "pyarrow"),
        default=os.environ.get("BENCH_DLT_BACKEND", "default"),
        help="dlt source backend mode: default uses dlt's out-of-box path; pyarrow opts into Arrow where supported",
    )
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
        source = mongodb_source(source_uri, args.source_table, args.backend)
    else:
        if args.backend == "default":
            source = sql_table(
                credentials=source_uri,
                table=src_table,
                schema=src_schema,
            )
        else:
            from sqlalchemy import Float, Text

            def adapt_column_types(table):
                for col in table.columns:
                    if str(col.type) == "DOUBLE":
                        col.type = Float()
                    elif str(col.type).upper() in ("JSON", "JSONB"):
                        col.type = Text()

            source_kwargs = dict(
                credentials=source_uri,
                table=src_table,
                schema=src_schema,
                table_adapter_callback=adapt_column_types,
            )
            source_kwargs["backend"] = "pyarrow"

            source = sql_table(**source_kwargs)

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
    elif dest_uri.startswith(("mssql://", "sqlserver://", "mssql+pyodbc://")):
        patch_dlt_mssql_json_type()
        destination = dlt.destinations.mssql(credentials=normalize_mssql_uri(dest_uri))
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

# /// script
# requires-python = ">=3.12,<3.13"
# dependencies = [
#     "airbyte==0.47.0",
# ]
# ///
"""Benchmark script for Airbyte (PyAirbyte)."""

import argparse
import os
import sys
import tempfile

os.environ["AIRBYTE_ANALYTICS_DISABLED"] = "1"
os.environ.setdefault("DO_NOT_TRACK", "1")

import airbyte as ab
from airbyte._message_iterators import AirbyteMessageIterator


def _patch_namespace_bug():
    """Monkey-patch PyAirbyte's from_read_result to include namespace on records.

    PyAirbyte 0.38.0 emits records with namespace=None from cache, but the catalog
    has namespace from the source (e.g. "public"). The destination connector rejects
    records that don't match the catalog namespace.
    """
    import datetime
    from typing import Generator, cast
    from airbyte_protocol.models import AirbyteMessage, AirbyteRecordMessage, Type

    AB_EXTRACTED_AT_COLUMN = "_airbyte_extracted_at"

    @classmethod
    def patched_from_read_result(cls, read_result):
        from airbyte._message_iterators import _new_stream_success_message
        from airbyte_protocol.models import (
            AirbyteStreamStatus,
            AirbyteStreamStatusTraceMessage,
            AirbyteTraceMessage,
            StreamDescriptor,
            TraceType,
        )
        from airbyte_cdk.utils.datetime_helpers import ab_datetime_now

        state_provider = read_result.cache.get_state_provider(
            source_name=read_result.source_name,
            refresh=True,
        )

        # Build a stream_name -> namespace map from stream metadata
        ns_map = {}
        for dataset in read_result.values():
            meta = dataset._stream_metadata
            stream = meta.stream
            ns_map[stream.name] = stream.namespace

        def _stream_success_with_ns(stream_name, namespace):
            return AirbyteMessage(
                type=Type.TRACE,
                trace=AirbyteTraceMessage(
                    type=TraceType.STREAM_STATUS,
                    emitted_at=ab_datetime_now().timestamp(),
                    stream_status=AirbyteStreamStatusTraceMessage(
                        stream_descriptor=StreamDescriptor(
                            name=stream_name,
                            namespace=namespace,
                        ),
                        status=AirbyteStreamStatus.COMPLETE,
                        reasons=None,
                    ),
                    estimate=None,
                    error=None,
                ),
                log=None,
                record=None,
                state=None,
            )

        def generator() -> Generator[AirbyteMessage, None, None]:
            for stream_name, dataset in read_result.items():
                namespace = ns_map.get(stream_name)
                for record in dataset:
                    yield AirbyteMessage(
                        type=Type.RECORD,
                        record=AirbyteRecordMessage(
                            stream=stream_name,
                            data=record,
                            emitted_at=int(
                                cast(
                                    datetime.datetime, record.get(AB_EXTRACTED_AT_COLUMN)
                                ).timestamp()
                            ),
                            meta=None,
                            namespace=namespace,
                        ),
                    )
                if stream_name in state_provider.known_stream_names:
                    yield AirbyteMessage(
                        type=Type.STATE,
                        state=state_provider.get_stream_state(stream_name),
                    )
                yield _stream_success_with_ns(stream_name, namespace)

        return cls(generator())

    AirbyteMessageIterator.from_read_result = patched_from_read_result


_patch_namespace_bug()


def docker_host(hostname: str) -> str:
    """Airbyte connectors run in Docker, so localhost must be remapped to the host IP.

    On macOS (Docker Desktop), host.docker.internal works automatically.
    On Linux, it doesn't — use the Docker bridge gateway IP instead.
    """
    if hostname not in ("localhost", "127.0.0.1"):
        return hostname

    import platform

    if platform.system() == "Darwin":
        return "host.docker.internal"

    # Linux: get docker bridge gateway IP
    import subprocess

    try:
        result = subprocess.run(
            ["docker", "network", "inspect", "bridge",
             "--format", "{{range .IPAM.Config}}{{.Gateway}}{{end}}"],
            capture_output=True, text=True, timeout=5,
        )
        if result.returncode == 0 and result.stdout.strip():
            return result.stdout.strip()
    except Exception:
        pass

    return "172.17.0.1"


def parse_postgres_uri(uri: str) -> dict:
    from urllib.parse import urlparse

    uri = uri.replace("postgresql://", "postgres://", 1)
    p = urlparse(uri)
    return {
        "host": docker_host(p.hostname),
        "port": p.port or 5432,
        "database": p.path.lstrip("/"),
        "username": p.username,
        "password": p.password,
        "schemas": ["public"],
        "ssl_mode": {"mode": "disable"},
    }


def parse_mysql_uri(uri: str) -> dict:
    from urllib.parse import urlparse

    p = urlparse(uri)
    return {
        "host": docker_host(p.hostname),
        "port": p.port or 3306,
        "database": p.path.lstrip("/"),
        "username": p.username,
        "password": p.password,
        "replication_method": {"method": "STANDARD"},
    }


def parse_mongodb_uri(uri: str, source_table: str) -> dict:
    from urllib.parse import parse_qsl, urlencode, urlparse

    collection_part, _, _ = source_table.partition(":")
    if "." not in collection_part:
        raise ValueError(f"MongoDB source table must be database.collection, got: {source_table}")
    database, _ = collection_part.split(".", 1)

    p = urlparse(uri)
    if p.scheme == "mongodb+srv":
        connection_string = uri
    else:
        auth = ""
        if p.username:
            auth = p.username
            if p.password:
                auth += f":{p.password}"
            auth += "@"

        query = dict(parse_qsl(p.query, keep_blank_values=True))
        query.setdefault("directConnection", "true")
        query.setdefault("replicaSet", "rs0")
        connection_string = (
            f"{p.scheme}://{auth}{docker_host(p.hostname)}:{p.port or 27017}/?"
            f"{urlencode(query)}"
        )

    return {
        "database_config": {
            "cluster_type": "SELF_MANAGED_REPLICA_SET",
            "connection_string": connection_string,
            "databases": [database],
            "schema_enforced": False,
        },
        "initial_waiting_seconds": 120,
        "discover_sample_size": 1000,
        "discover_timeout_seconds": 60,
    }


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--source-uri", required=True)
    parser.add_argument("--source-table", required=True)
    parser.add_argument("--dest-uri", required=True)
    parser.add_argument("--dest-table", required=True)
    args = parser.parse_args()

    if "." in args.source_table:
        _, src_table = args.source_table.split(".", 1)
    else:
        src_table = args.source_table

    # DuckDB source not supported by Airbyte
    if args.source_uri.startswith("duckdb://"):
        print("SKIP: Airbyte has no source-duckdb connector", file=sys.stderr)
        sys.exit(1)

    # DuckDB destination connector runs in Docker and can't access host paths
    if args.dest_uri.startswith("duckdb://"):
        print("SKIP: Airbyte destination-duckdb runs in Docker, can't access host paths", file=sys.stderr)
        sys.exit(1)

    # BigQuery destination needs credentials mounted into Docker — not yet supported
    if args.dest_uri.startswith("bigquery://"):
        print("SKIP: Airbyte destination-bigquery requires Docker credential mounting (not yet implemented)", file=sys.stderr)
        sys.exit(1)

    # Build source
    if args.source_uri.startswith(("postgres://", "postgresql://")):
        source = ab.get_source("source-postgres", config=parse_postgres_uri(args.source_uri))
    elif args.source_uri.startswith("mysql://"):
        source = ab.get_source("source-mysql", config=parse_mysql_uri(args.source_uri))
    elif args.source_uri.startswith(("mongodb://", "mongodb+srv://")):
        source = ab.get_source("source-mongodb-v2", config=parse_mongodb_uri(args.source_uri, args.source_table))
    else:
        print(f"Unsupported source: {args.source_uri}", file=sys.stderr)
        sys.exit(1)

    source.select_streams([src_table])

    # Read source into a local cache first (workaround for namespace mismatch
    # between source records and destination catalog in PyAirbyte).
    cache = ab.new_local_cache()
    read_result = source.read(cache, force_full_refresh=True)

    # Build destination
    dest_uri = args.dest_uri
    if dest_uri.startswith(("postgres://", "postgresql://")):
        dst_pg = parse_postgres_uri(dest_uri)
        destination = ab.get_destination(
            "destination-postgres",
            config={
                "host": dst_pg["host"],
                "port": dst_pg["port"],
                "database": dst_pg["database"],
                "username": dst_pg["username"],
                "password": dst_pg["password"],
                "schema": "public",
                "ssl_mode": {"mode": "disable"},
            },
        )
    elif dest_uri.startswith("duckdb://"):
        db_path = dest_uri.split("duckdb:///", 1)[1]
        destination = ab.get_destination(
            "destination-duckdb",
            config={"destination_path": db_path},
        )
    else:
        print(f"Unsupported destination: {dest_uri}", file=sys.stderr)
        sys.exit(1)

    result = destination.write(
        read_result,
        force_full_refresh=True,
    )
    print(result)


if __name__ == "__main__":
    main()

import base64
import csv
import gzip
import io
import json
import logging
import os
import random
import shutil
import string
import tempfile
import time
import traceback
import urllib.request
from concurrent.futures import ThreadPoolExecutor
from dataclasses import dataclass
from datetime import date, datetime, timezone
from typing import Callable, Dict, Iterable, List, Optional
from unittest.mock import MagicMock, patch
from urllib.parse import urlparse

import duckdb
import numpy as np
import pandas as pd  # type: ignore
import pendulum
import pyarrow as pa  # type: ignore
import pyarrow.csv  # type: ignore
import pyarrow.ipc as ipc  # type: ignore
import pyarrow.parquet as pya_parquet  # type: ignore
import pytest
import requests
import sqlalchemy
from confluent_kafka import Producer  # type: ignore
from cratedb_toolkit.testing.testcontainers.cratedb import (  # type: ignore
    CrateDBContainer,
)
from dlt.sources.filesystem import glob_files
from elasticsearch import Elasticsearch
from fsspec.implementations.memory import MemoryFileSystem  # type: ignore
from sqlalchemy.pool import NullPool
from testcontainers.clickhouse import ClickHouseContainer  # type: ignore
from testcontainers.core.container import DockerContainer  # type: ignore
from testcontainers.core.waiting_utils import wait_for_logs  # type: ignore
from testcontainers.elasticsearch import ElasticSearchContainer  # type: ignore
from testcontainers.kafka import KafkaContainer  # type: ignore
from testcontainers.localstack import LocalStackContainer  # type: ignore
from testcontainers.mongodb import MongoDbContainer  # type: ignore
from testcontainers.mssql import SqlServerContainer  # type: ignore
from testcontainers.mysql import MySqlContainer  # type: ignore
from testcontainers.postgres import PostgresContainer  # type: ignore
from typer.testing import CliRunner
from yarl import URL

from ingestr.main import app
from ingestr.src.appstore.errors import (
    NoOngoingReportRequestsFoundError,
    NoReportsFoundError,
    NoSuchReportError,
)
from ingestr.src.appstore.models import (
    AnalyticsReportInstancesResponse,
    AnalyticsReportRequestsResponse,
    AnalyticsReportResponse,
    AnalyticsReportSegmentsResponse,
    Report,
    ReportAttributes,
    ReportInstance,
    ReportInstanceAttributes,
    ReportRequest,
    ReportRequestAttributes,
    ReportSegment,
    ReportSegmentAttributes,
)
from ingestr.src.destinations import (
    ClickhouseDestination,
    S3Destination,
)
from ingestr.src.errors import (
    InvalidBlobTableError,
    MissingValueError,
    UnsupportedResourceError,
)

logging.getLogger("testcontainers.core.waiting_utils").setLevel(logging.WARNING)
logging.getLogger("testcontainers.core.container").setLevel(logging.WARNING)


def has_exception(exception, exc_type):
    if isinstance(exception, pytest.ExceptionInfo):
        exception = exception.value

    while exception:
        if isinstance(exception, exc_type):
            return True
        exception = exception.__cause__
    return False


def get_abs_path(relative_path):
    return os.path.abspath(os.path.join(os.path.dirname(__file__), relative_path))


def invoke_ingest_command(
    source_uri,
    source_table,
    dest_uri,
    dest_table,
    inc_strategy=None,
    inc_key=None,
    primary_key=None,
    merge_key=None,
    interval_start=None,
    interval_end=None,
    sql_backend=None,
    loader_file_format=None,
    sql_exclude_columns=None,
    columns=None,
    sql_limit=None,
    yield_limit=None,
    mask=None,
    print_output=True,
    run_in_subprocess=False,
):
    args = [
        "ingest",
        "--source-uri",
        source_uri,
        "--source-table",
        source_table,
        "--dest-uri",
        dest_uri,
        "--dest-table",
        dest_table,
    ]

    if inc_strategy:
        args.append("--incremental-strategy")
        args.append(inc_strategy)

    if inc_key:
        args.append("--incremental-key")
        args.append(inc_key)

    if primary_key:
        args.append("--primary-key")
        args.append(primary_key)

    if merge_key:
        args.append("--merge-key")
        args.append(merge_key)

    if interval_start:
        args.append("--interval-start")
        args.append(interval_start)

    if interval_end:
        args.append("--interval-end")
        args.append(interval_end)

    if sql_backend:
        args.append("--sql-backend")
        args.append(sql_backend)

    if loader_file_format:
        args.append("--loader-file-format")
        args.append(loader_file_format)

    if sql_exclude_columns:
        args.append("--sql-exclude-columns")
        args.append(sql_exclude_columns)

    if columns:
        args.append("--columns")
        args.append(columns)

    if sql_limit:
        args.append("--sql-limit")
        args.append(sql_limit)

    if yield_limit:
        args.append("--yield-limit")
        args.append(str(yield_limit))

    if mask:
        if isinstance(mask, str):
            mask = [mask]
        for m in mask:
            args.append("--mask")
            args.append(m)

    if not run_in_subprocess:
        result = CliRunner().invoke(
            app,
            args,
            input="y\n",
            env={"DISABLE_TELEMETRY": "true"},
        )
        if result.exit_code != 0 and print_output:
            traceback.print_exception(*result.exc_info)

        return result

    import subprocess
    import sys

    cmd = [sys.executable, "-m", "ingestr.main"] + args
    env = os.environ.copy()
    env["DISABLE_TELEMETRY"] = "true"

    process = subprocess.run(cmd, input="y\n", text=True, capture_output=True, env=env)

    # Create a result object similar to what CliRunner returns
    class Result:
        def __init__(self, exit_code, stdout, stderr, exc_info=None):
            self.exit_code = exit_code
            self.stdout = stdout
            self.stderr = stderr
            self.exc_info = exc_info

    result = Result(process.returncode, process.stdout, process.stderr)

    if result.exit_code != 0 and print_output:
        print(result.stdout)
        print(result.stderr)
        # traceback.print_exception(result.exc_info)

    return result


### These are CSV-to-DuckDB tests
def test_create_replace_csv_to_duckdb():
    try:
        shutil.rmtree(get_abs_path("../pipeline_data"))
    except Exception:
        pass

    abs_db_path = get_abs_path("./testdata/test_create_replace_csv.db")
    rel_db_path_to_command = "ingestr/testdata/test_create_replace_csv.db"
    rel_source_path_to_command = "ingestr/testdata/create_replace.csv"

    conn = duckdb.connect(abs_db_path)

    result = invoke_ingest_command(
        f"csv://{rel_source_path_to_command}",
        "testschema.input",
        f"duckdb:///{rel_db_path_to_command}",
        "testschema.output",
    )

    assert result.exit_code == 0

    res = conn.sql(
        "select symbol, date, is_enabled, name from testschema.output"
    ).fetchall()

    # read CSV file
    actual_rows = []
    with open(get_abs_path("./testdata/create_replace.csv"), "r") as f:
        reader = csv.reader(f, delimiter=",", quotechar='"')
        next(reader, None)
        for row in reader:
            actual_rows.append([None if v.strip() == "" else v for v in row])

    # compare the CSV file with the DuckDB table
    assert len(res) == len(actual_rows)
    for i, row in enumerate(actual_rows):
        assert res[i] == tuple(row)

    # Clean up
    conn.close()
    try:
        os.remove(abs_db_path)
    except Exception:
        pass


def get_random_string(length):
    letters = string.ascii_lowercase
    result_str = "".join(random.choice(letters) for i in range(length))
    return result_str


def test_merge_with_primary_key_csv_to_duckdb():
    try:
        shutil.rmtree(get_abs_path("../pipeline_data"))
    except Exception:
        pass

    dbname = f"test_merge_with_primary_key_csv{get_random_string(5)}.db"
    abs_db_path = get_abs_path(f"./testdata/{dbname}")
    rel_db_path_to_command = f"ingestr/testdata/{dbname}"
    uri = f"duckdb:///{rel_db_path_to_command}"

    # DuckDB is sensitive about multiple connections to the same database file.
    # Connection Error: Can't open a connection to same database file with a
    # different configuration than existing connections
    # conn = duckdb.connect(abs_db_path)

    def run(source: str):
        res = invoke_ingest_command(
            source,
            "whatever",  # table name doesnt matter for CSV
            uri,
            "testschema_merge.output",
            "merge",
            "date",
            "symbol",
        )
        assert res.exit_code == 0
        return res

    def get_output_rows():
        conn = duckdb.connect(abs_db_path)
        conn.execute("CHECKPOINT")
        results = conn.sql(
            "select symbol, date, is_enabled, name from testschema_merge.output order by symbol asc"
        ).fetchall()
        conn.close()
        return results

    def assert_output_equals_to_csv(path: str):
        res = get_output_rows()
        actual_rows = []
        with open(get_abs_path(path), "r") as f:
            reader = csv.reader(f, delimiter=",", quotechar='"')
            next(reader, None)
            for row in reader:
                actual_rows.append(row)

        assert len(res) == len(actual_rows)
        for i, row in enumerate(actual_rows):
            assert res[i] == tuple(row)

    run("csv://ingestr/testdata/merge_part1.csv")
    assert_output_equals_to_csv("./testdata/merge_part1.csv")

    conn = duckdb.connect(abs_db_path)
    first_run_id = conn.sql(
        "select _dlt_load_id from testschema_merge.output limit 1"
    ).fetchall()[0][0]
    conn.close()

    ##############################
    # we'll run again, we don't expect any changes since the data hasn't changed
    run("csv://ingestr/testdata/merge_part1.csv")
    assert_output_equals_to_csv("./testdata/merge_part1.csv")

    # we also ensure that the other rows were not touched
    conn = duckdb.connect(abs_db_path)
    count_by_run_id = conn.sql(
        "select _dlt_load_id, count(*) from testschema_merge.output group by 1"
    ).fetchall()
    conn.close()
    assert len(count_by_run_id) == 1
    assert count_by_run_id[0][1] == 3
    assert count_by_run_id[0][0] == first_run_id
    ##############################

    ##############################
    # now we'll run the same ingestion but with a different file this time

    run("csv://ingestr/testdata/merge_part2.csv")
    assert_output_equals_to_csv("./testdata/merge_expected.csv")

    # let's check the runs
    conn = duckdb.connect(abs_db_path)
    count_by_run_id = conn.sql(
        "select _dlt_load_id, count(*) from testschema_merge.output group by 1 order by 1 asc"
    ).fetchall()
    conn.close()

    # we expect that there's a new load ID now
    assert len(count_by_run_id) == 2

    # there should be only one row with the first load ID
    assert count_by_run_id[0][1] == 1
    assert count_by_run_id[0][0] == first_run_id

    # there should be a new run with the rest, 2 rows updated + 1 new row
    assert count_by_run_id[1][1] == 3
    ##############################

    try:
        os.remove(abs_db_path)
    except Exception:
        pass


def test_delete_insert_without_primary_key_csv_to_duckdb():
    try:
        shutil.rmtree(get_abs_path("../pipeline_data"))
    except Exception:
        pass

    dbname = f"test_merge_with_primary_key_csv{get_random_string(5)}.db"
    abs_db_path = get_abs_path(f"./testdata/{dbname}")
    rel_db_path_to_command = f"ingestr/testdata/{dbname}"
    uri = f"duckdb:///{rel_db_path_to_command}"

    conn = duckdb.connect(abs_db_path)

    def run(source: str):
        res = invoke_ingest_command(
            source,
            "whatever",  # table name doesnt matter for CSV
            uri,
            "testschema.output",
            "delete+insert",
            "date",
        )
        assert res.exit_code == 0
        return res

    def get_output_rows():
        conn.execute("CHECKPOINT")
        return conn.sql(
            "select symbol, date, is_enabled, name from testschema.output order by symbol asc"
        ).fetchall()

    def assert_output_equals_to_csv(path: str):
        res = get_output_rows()
        actual_rows = []
        with open(get_abs_path(path), "r") as f:
            reader = csv.reader(f, delimiter=",", quotechar='"')
            next(reader, None)
            for row in reader:
                actual_rows.append(row)

        assert len(res) == len(actual_rows)
        for i, row in enumerate(actual_rows):
            assert res[i] == tuple(row)

    run("csv://ingestr/testdata/delete_insert_part1.csv")
    assert_output_equals_to_csv("./testdata/delete_insert_part1.csv")

    first_run_id = conn.sql(
        "select _dlt_load_id from testschema.output limit 1"
    ).fetchall()[0][0]

    ##############################
    # we'll run again, we expect the data to be the same, but a new load_id to exist
    # this is due to the fact that the old data won't be touched, but the ones with the
    # latest value will be rewritten
    run("csv://ingestr/testdata/delete_insert_part1.csv")
    assert_output_equals_to_csv("./testdata/delete_insert_part1.csv")

    # we also ensure that the other rows were not touched
    count_by_run_id = conn.sql(
        "select _dlt_load_id, count(*) from testschema.output group by 1 order by 1 asc"
    ).fetchall()

    assert len(count_by_run_id) == 2
    assert count_by_run_id[0][1] == 1
    assert count_by_run_id[0][0] == first_run_id
    assert count_by_run_id[1][1] == 3
    ##############################

    ##############################
    # now we'll run the same ingestion but with a different file this time

    run("csv://ingestr/testdata/delete_insert_part2.csv")
    assert_output_equals_to_csv("./testdata/delete_insert_expected.csv")

    # let's check the runs
    count_by_run_id = conn.sql(
        "select _dlt_load_id, count(*) from testschema.output group by 1 order by 1 asc"
    ).fetchall()

    # we expect that there's a new load ID now
    assert len(count_by_run_id) == 2

    # there should be only one row with the first load ID, oldest date
    assert count_by_run_id[0][1] == 1
    assert count_by_run_id[0][0] == first_run_id

    # there should be a new run with the rest, 3 rows updated + 1 new row
    assert count_by_run_id[1][1] == 4
    ##############################

    # Clean up
    conn.close()
    try:
        os.remove(abs_db_path)
    except Exception:
        pass


class DockerImage:
    def __init__(self, id: str, container_creator, connection_suffix: str = "") -> None:
        self.id = id
        self.container_creator = container_creator
        self.connection_suffix = connection_suffix
        self.container_lock_dir = None
        self.container = None

    def start(self) -> str:
        file_path = f"{self.container_lock_dir}/{self.id}"
        attempts = 0
        while self.container_lock_dir is None or not os.path.exists(file_path):
            time.sleep(1)
            attempts += 1
            if attempts > 20:
                raise Exception("Failed to start container after bunch of attempts")

        with open(file_path, "r") as f:
            res = f.read()
            return res

    def start_fully(self) -> str:
        self.container = self.container_creator()
        if self.container is None:
            raise ValueError("Container is not initialized.")

        conn_url = self.container.get_connection_url() + self.connection_suffix

        with open(f"{self.container_lock_dir}/{self.id}", "w") as f:
            f.write(conn_url)

        return conn_url

    def stop(self):
        pass

    def stop_fully(self):
        if self.container is not None:
            self.container.stop()


class ClickhouseDockerImage(DockerImage):
    def start_fully(self) -> str:
        self.container = self.container_creator()
        if self.container is None:
            raise ValueError("Container is not initialized.")

        port = self.container.get_exposed_port(8123)
        conn_url = (
            self.container.get_connection_url().replace(
                "clickhouse://", "clickhouse+native://"
            )
            + f"?http_port={port}&secure=0"
        )
        # raise ValueError(conn_url)
        with open(f"{self.container_lock_dir}/{self.id}", "w") as f:
            f.write(conn_url)

        return conn_url


class CrateDbDockerImage(DockerImage):
    """
    The CrateDB destination uses the PostgreSQL protocol (default port 5432).
    """

    def start_fully(self) -> str:
        self.container = self.container_creator()
        if self.container is None:
            raise ValueError("Container is not initialized.")

        port5432 = int(self.container.get_exposed_port(5432))
        url = (
            URL(self.container.get_connection_url())
            .with_scheme("cratedb")
            .with_port(port5432)
        )

        conn_url = str(url)
        with open(f"{self.container_lock_dir}/{self.id}", "w") as f:
            f.write(conn_url)

        return conn_url


class EphemeralDuckDb:
    def __init__(self) -> None:
        self.abs_path = get_abs_path(f"./testdata/duckdb_{get_random_string(5)}.db")
        self.connection_url = f"duckdb:///{self.abs_path}"
        this = self

        class ContainerSurrogate:
            def get_connection_url(self) -> str:
                return this.connection_url

        self.container = ContainerSurrogate()

    def start(self) -> str:
        return self.connection_url

    def start_fully(self) -> str:  # type: ignore
        pass

    def stop(self):
        pass

    def stop_fully(self):
        # Get all duckdb_*.db files in the testdata directory and delete them
        testdata_dir = get_abs_path("./testdata/")
        for file in os.listdir(testdata_dir):
            if file.startswith("duckdb_") and file.endswith(".db"):
                try:
                    os.remove(os.path.join(testdata_dir, file))
                except Exception:
                    pass


class CouchbaseContainer(DockerContainer):
    """Custom Couchbase container for testing."""

    def __init__(self, image: str = "couchbase:community", **kwargs):
        super().__init__(image, **kwargs)
        # Use 1:1 port mapping (requires local Couchbase to be stopped)
        # This allows SDK to connect without alternate addresses
        self.with_bind_ports(8091, 8091)
        self.with_bind_ports(8092, 8092)
        self.with_bind_ports(8093, 8093)
        self.with_bind_ports(8094, 8094)
        self.with_bind_ports(8095, 8095)
        self.with_bind_ports(8096, 8096)
        self.with_bind_ports(11210, 11210)
        self.username = "Administrator"
        self.password = "password"
        self.bucket_name = "test_bucket"
        self.scope_name = "_default"
        self.collection_name = "_default"

    def start(self):
        """Start container and initialize Couchbase."""
        super().start()

        # Wait for Couchbase web console to be ready
        self._wait_for_couchbase()

        # Initialize cluster
        self._initialize_cluster()

        # Create bucket
        self._create_bucket()

        # Wait for bucket to be ready
        time.sleep(10)

        # Create primary index for N1QL queries
        self._create_primary_index()

        return self

    def _wait_for_couchbase(self):
        """Wait for Couchbase to be ready."""
        import requests

        port = self.get_exposed_port(8091)
        base_url = f"http://{self.get_container_host_ip()}:{port}"

        max_attempts = 30
        for i in range(max_attempts):
            try:
                response = requests.get(f"{base_url}/pools", timeout=2)
                if response.status_code == 200:
                    return
            except Exception:
                pass
            time.sleep(2)

        raise Exception(f"Couchbase did not become ready after {max_attempts} attempts")

    def _initialize_cluster(self):
        """Initialize Couchbase cluster using couchbase-cli."""
        # Use couchbase-cli inside the container for proper setup
        self.exec(
            f"couchbase-cli cluster-init -c 127.0.0.1 "
            f"--cluster-username {self.username} "
            f"--cluster-password {self.password} "
            f"--services data,index,query "
            f"--cluster-ramsize 256 "
            f"--cluster-index-ramsize 256"
        )

        # Wait for cluster to be initialized
        time.sleep(5)

    def _setup_alternate_addresses(self):
        """Setup alternate addresses for SDK bootstrap."""
        import requests

        host = self.get_container_host_ip()
        port = self.get_exposed_port(8091)

        # Configure alternate addresses so SDK can connect from outside container
        requests.post(
            f"http://{host}:{port}/node/controller/setupAlternateAddresses/external",
            auth=(self.username, self.password),
            json={
                "hostname": host,
                "mgmt": int(self.get_exposed_port(8091)),
                "kv": int(self.get_exposed_port(11210)),
                "n1ql": int(self.get_exposed_port(8093)),
                "capi": int(self.get_exposed_port(8092)),
                "fts": int(self.get_exposed_port(8094)),
                "cbas": int(self.get_exposed_port(8095)),
                "eventingAdminPort": int(self.get_exposed_port(8096)),
            },
        )
        time.sleep(2)

    def _create_bucket(self):
        """Create a test bucket using couchbase-cli."""
        self.exec(
            f"couchbase-cli bucket-create -c 127.0.0.1 "
            f"-u {self.username} -p {self.password} "
            f"--bucket {self.bucket_name} "
            f"--bucket-type couchbase "
            f"--bucket-ramsize 100 "
            f"--storage-backend couchstore "  # Use couchstore for community edition
            f"--bucket-replica 0"  # No replicas for single node
        )

        # Wait for bucket to be ready and healthy
        self._wait_for_bucket_ready()

    def _wait_for_bucket_ready(self):
        """Wait for bucket to be healthy and ready."""
        import requests

        host = self.get_container_host_ip()
        port = self.get_exposed_port(8091)

        for i in range(30):
            try:
                response = requests.get(
                    f"http://{host}:{port}/pools/default/buckets/{self.bucket_name}",
                    auth=(self.username, self.password),
                    timeout=2,
                )
                if response.status_code == 200:
                    bucket_info = response.json()
                    # Check if bucket is healthy and all nodes are ready
                    if bucket_info.get("nodes") and all(
                        node.get("status") == "healthy" for node in bucket_info["nodes"]
                    ):
                        time.sleep(5)  # Extra wait for full readiness
                        return
            except Exception:
                pass
            time.sleep(2)

        raise Exception(
            f"Bucket '{self.bucket_name}' did not become ready after waiting"
        )

    def _create_primary_index(self):
        """Create primary index for N1QL queries using cbq CLI."""
        # Use cbq command-line tool to create the primary index
        # Note: We ignore errors if the index already exists
        query = f"CREATE PRIMARY INDEX ON `{self.bucket_name}`.`{self.scope_name}`.`{self.collection_name}`"
        try:
            self.exec(
                f"cbq -u {self.username} -p {self.password} -engine=http://127.0.0.1:8091/ "
                f'-script="{query}"'
            )
            time.sleep(2)
        except Exception:
            # Index may already exist, ignore error
            pass

    def get_connection_string(self) -> str:
        """Get Couchbase connection string."""
        # With 1:1 port mapping, use localhost
        return "couchbase://localhost"

    def get_connection_url(self) -> str:
        """Get connection URL with credentials."""
        # With 1:1 port mapping, use localhost
        return f"couchbase://{self.username}:{self.password}@localhost"

    def insert_documents(self, documents: list):
        """Insert documents using Couchbase Python SDK from test machine."""
        from datetime import timedelta

        from couchbase.auth import PasswordAuthenticator  # type: ignore
        from couchbase.cluster import Cluster  # type: ignore
        from couchbase.options import ClusterOptions  # type: ignore

        # Connect using SDK (from test machine to container)
        auth = PasswordAuthenticator(self.username, self.password)
        cluster = Cluster(self.get_connection_string(), ClusterOptions(auth))
        cluster.wait_until_ready(timedelta(seconds=30))

        # Get bucket and collection
        bucket = cluster.bucket(self.bucket_name)
        collection = bucket.scope(self.scope_name).collection(self.collection_name)

        # Insert documents
        for doc in documents:
            doc_id = str(doc.get("id", doc.get("_id", f"doc_{hash(str(doc))}")))
            collection.upsert(doc_id, doc)

        time.sleep(2)


POSTGRES_IMAGE = "postgres:16.3-alpine3.20"
MYSQL8_IMAGE = "mysql:8.4.1"
MSSQL22_IMAGE = "mcr.microsoft.com/mssql/server:2022-CU13-ubuntu-22.04"
CLICKHOUSE_IMAGE = "clickhouse/clickhouse-server:24.12"
MONGODB_IMAGE = "mongo:8.0.13"
COUCHBASE_IMAGE = "couchbase:community"
CRATEDB_IMAGE = "crate:5.10"

pgDocker = DockerImage(
    "postgres", lambda: PostgresContainer(POSTGRES_IMAGE, driver=None).start()
)
clickHouseDocker = ClickhouseDockerImage(
    "clickhouse", lambda: ClickHouseContainer(CLICKHOUSE_IMAGE).start()
)
crateDbDocker = CrateDbDockerImage(
    "cratedb",
    lambda: CrateDBContainer(CRATEDB_IMAGE, ports={4200: None, 5432: None}).start(),
)
mysqlDocker = DockerImage(
    "mysql", lambda: MySqlContainer(MYSQL8_IMAGE, username="root").start()
)


@pytest.fixture(scope="session")
def mongodb_server():
    container = MongoDbContainer(MONGODB_IMAGE)
    container.start()
    yield container
    container.stop()


SOURCES = {
    "postgres": pgDocker,
    "duckdb": EphemeralDuckDb(),
    "mysql8": mysqlDocker,
    "sqlserver": DockerImage(
        "sqlserver",
        lambda: SqlServerContainer(MSSQL22_IMAGE, dialect="mssql").start(),
        "?driver=ODBC+Driver+18+for+SQL+Server&TrustServerCertificate=Yes",
    ),
}

DESTINATIONS = {
    "postgres": pgDocker,
    "duckdb": EphemeralDuckDb(),
    "clickhouse+native": clickHouseDocker,
}


@pytest.fixture(scope="session", autouse=True)
def manage_containers(request, shared_directory):
    unique_containers = set(SOURCES.values()) | set(DESTINATIONS.values())
    for container in unique_containers:
        container.container_lock_dir = shared_directory


@pytest.fixture(scope="session", autouse=True)
def autocreate_db_for_clickhouse():
    """
    patches ClickhouseDestination to autocreate dest tables if they don't exist
    """
    dlt_dest = ClickhouseDestination().dlt_dest

    def patched_dlt_dest(uri, **kwargs):
        db, _ = kwargs["dest_table"].split(".")
        dest_engine = sqlalchemy.create_engine(uri)
        dest_engine.execute(f"CREATE DATABASE IF NOT EXISTS {db}")
        return dlt_dest(uri, **kwargs)

    patcher = patch("ingestr.src.factory.ClickhouseDestination.dlt_dest")
    mock = patcher.start()
    mock.side_effect = patched_dlt_dest
    yield
    patcher.stop()


def get_uri_read(url: str, image: DockerImage) -> str:
    """
    Some databases need different URLs (destination vs. reading back).

    CrateDB uses `cratedb://` for destination addressing,
    but `crate://` for reading back, as the latter is the
    designated scheme of the SQLAlchemy dialect.

    In abundance to that, `cratedb://` uses the PostgreSQL protocol
    (default port 5432), while `crate://` uses the HTTP protocol
    (default port 4200).

    C'est la vie. ¯\\_(ツ)_/¯
    """
    uri = URL(url)
    if uri.scheme == "cratedb":
        if image.container is None:
            raise RuntimeError("Needs a container to determine exposed port")
        port4200 = int(image.container.get_exposed_port(4200))
        uri = uri.with_scheme("crate").with_port(port4200)
        return str(uri)
    return url


@pytest.mark.parametrize(
    "dest", list(DESTINATIONS.values()), ids=list(DESTINATIONS.keys())
)
@pytest.mark.parametrize("source", list(SOURCES.values()), ids=list(SOURCES.keys()))
def test_create_replace(source, dest):
    if isinstance(source.container, SqlServerContainer) and isinstance(
        dest, CrateDbDockerImage
    ):
        pytest.skip(
            "CrateDB type mapping does not support `DATE` yet, "
            "see https://github.com/crate-workbench/ingestr/issues/4"
        )
    with ThreadPoolExecutor() as executor:
        source_future = executor.submit(source.start)
        dest_future = executor.submit(dest.start)
        source_uri = source_future.result()
        dest_uri = dest_future.result()
    dest_uri_read = get_uri_read(dest_uri, dest)
    db_to_db_create_replace(source_uri, dest_uri, dest_uri_read)
    source.stop()
    dest.stop()


@pytest.mark.parametrize(
    "dest", list(DESTINATIONS.values()), ids=list(DESTINATIONS.keys())
)
@pytest.mark.parametrize("source", list(SOURCES.values()), ids=list(SOURCES.keys()))
def test_append(source, dest):
    if isinstance(dest, CrateDbDockerImage):
        pytest.skip(
            "CrateDB support for 'append' strategy pending, "
            "see https://github.com/crate-workbench/ingestr/issues/6"
        )
    with ThreadPoolExecutor() as executor:
        source_future = executor.submit(source.start)
        dest_future = executor.submit(dest.start)
        source_uri = source_future.result()
        dest_uri = dest_future.result()
    dest_uri_read = get_uri_read(dest_uri, dest)
    db_to_db_append(source_uri, dest_uri, dest_uri_read)
    source.stop()
    dest.stop()


@pytest.mark.parametrize(
    "dest", list(DESTINATIONS.values()), ids=list(DESTINATIONS.keys())
)
@pytest.mark.parametrize("source", list(SOURCES.values()), ids=list(SOURCES.keys()))
def test_merge_with_primary_key(source, dest):
    if isinstance(dest, CrateDbDockerImage):
        pytest.skip(
            "CrateDB support for 'merge' strategy pending, "
            "see https://github.com/crate/dlt-cratedb/issues/14"
        )
    with ThreadPoolExecutor() as executor:
        source_future = executor.submit(source.start)
        dest_future = executor.submit(dest.start)
        source_uri = source_future.result()
        dest_uri = dest_future.result()
    dest_uri_read = get_uri_read(dest_uri, dest)
    db_to_db_merge_with_primary_key(source_uri, dest_uri, dest_uri_read)
    source.stop()
    dest.stop()


@pytest.mark.parametrize(
    "dest", list(DESTINATIONS.values()), ids=list(DESTINATIONS.keys())
)
@pytest.mark.parametrize("source", list(SOURCES.values()), ids=list(SOURCES.keys()))
def test_delete_insert_without_primary_key(source, dest):
    if isinstance(dest, CrateDbDockerImage):
        pytest.skip(
            "CrateDB support for 'delete+insert' strategy pending, "
            "see https://github.com/crate/dlt-cratedb/issues/14"
        )
    with ThreadPoolExecutor() as executor:
        source_future = executor.submit(source.start)
        dest_future = executor.submit(dest.start)
        source_uri = source_future.result()
        dest_uri = dest_future.result()
    dest_uri_read = get_uri_read(dest_uri, dest)
    db_to_db_delete_insert_without_primary_key(source_uri, dest_uri, dest_uri_read)
    source.stop()
    dest.stop()


@pytest.mark.parametrize(
    "dest", list(DESTINATIONS.values()), ids=list(DESTINATIONS.keys())
)
@pytest.mark.parametrize("source", list(SOURCES.values()), ids=list(SOURCES.keys()))
def test_delete_insert_with_time_range(source, dest):
    if isinstance(dest, CrateDbDockerImage):
        pytest.skip(
            "CrateDB support for 'delete+insert' strategy pending, "
            "see https://github.com/crate/dlt-cratedb/issues/14"
        )
    with ThreadPoolExecutor() as executor:
        source_future = executor.submit(source.start)
        dest_future = executor.submit(dest.start)
        source_uri = source_future.result()
        dest_uri = dest_future.result()
    dest_uri_read = get_uri_read(dest_uri, dest)
    db_to_db_delete_insert_with_timerange(source_uri, dest_uri, dest_uri_read)
    source.stop()
    dest.stop()


def db_to_db_create_replace(
    source_connection_url: str,
    dest_connection_url: str,
    dest_connection_url_read: str,
):
    schema_rand_prefix = f"testschema_create_replace_{get_random_string(5)}"
    try:
        shutil.rmtree(get_abs_path("../pipeline_data"))
    except Exception:
        pass

    # CrateDB: Compensate for "Type `date` does not support storage".
    updated_at_type = "DATE"
    if dest_connection_url.startswith("cratedb://"):
        updated_at_type = "TIMESTAMP"

    source_engine = sqlalchemy.create_engine(source_connection_url)
    with source_engine.begin() as conn:
        conn.execute(f"DROP SCHEMA IF EXISTS {schema_rand_prefix}")
        conn.execute(f"CREATE SCHEMA {schema_rand_prefix}")
        conn.execute(
            f"CREATE TABLE {schema_rand_prefix}.input (id INTEGER, val VARCHAR(20), updated_at {updated_at_type})"
        )
        conn.execute(
            f"INSERT INTO {schema_rand_prefix}.input VALUES (1, 'val1', '2022-01-01')"
        )
        conn.execute(
            f"INSERT INTO {schema_rand_prefix}.input VALUES (2, 'val2', '2022-02-01')"
        )
        res = conn.execute(
            f"select count(*) from {schema_rand_prefix}.input"
        ).fetchall()
        assert res[0][0] == 2
    source_engine.dispose()

    result = invoke_ingest_command(
        source_connection_url,
        f"{schema_rand_prefix}.input",
        dest_connection_url,
        f"{schema_rand_prefix}.output",
    )

    assert result.exit_code == 0

    dest_engine = sqlalchemy.create_engine(dest_connection_url_read)
    # CrateDB needs an explicit flush to make data available for reads immediately.
    if dest_engine.dialect.name == "crate":
        dest_engine.execute(f"REFRESH TABLE {schema_rand_prefix}.output")
    res = dest_engine.execute(
        f"select id, val, updated_at from {schema_rand_prefix}.output"
    ).fetchall()
    dest_engine.dispose()

    assert len(res) == 2

    # Compensate for CrateDB types and insert order.
    if dest_connection_url.startswith("cratedb://"):
        assert (1, "val1", 1640995200000) in res
        assert (2, "val2", 1643673600000) in res
    else:
        assert res[0] == (1, "val1", as_datetime("2022-01-01"))
        assert res[1] == (2, "val2", as_datetime("2022-02-01"))


def db_to_db_append(
    source_connection_url: str,
    dest_connection_url: str,
    dest_connection_url_read: str,
):
    schema_rand_prefix = f"testschema_append_{get_random_string(5)}"
    try:
        shutil.rmtree(get_abs_path("../pipeline_data"))
    except Exception:
        pass

    # CrateDB: Compensate for "Type `date` does not support storage".
    updated_at_type = "DATE"
    if dest_connection_url.startswith("cratedb://"):
        updated_at_type = "TIMESTAMP"

    source_engine = sqlalchemy.create_engine(source_connection_url)
    with source_engine.begin() as conn:
        conn.execute(f"DROP SCHEMA IF EXISTS {schema_rand_prefix}")
        conn.execute(f"CREATE SCHEMA {schema_rand_prefix}")
        conn.execute(
            f"CREATE TABLE {schema_rand_prefix}.input (id INTEGER, val VARCHAR(20), updated_at {updated_at_type})"
        )
        conn.execute(
            f"INSERT INTO {schema_rand_prefix}.input VALUES (1, 'val1', '2022-01-01'), (2, 'val2', '2022-01-02')"
        )
        res = conn.execute(
            f"select count(*) from {schema_rand_prefix}.input"
        ).fetchall()
        assert res[0][0] == 2
    source_engine.dispose()

    def run():
        res = invoke_ingest_command(
            source_connection_url,
            f"{schema_rand_prefix}.input",
            dest_connection_url,
            f"{schema_rand_prefix}.output",
            "append",
            "updated_at",
            sql_backend="sqlalchemy",
        )
        assert res.exit_code == 0

    def get_output_table():
        dest_engine = sqlalchemy.create_engine(dest_connection_url_read)
        # CrateDB needs an explicit flush to make data available for reads immediately.
        if dest_engine.dialect.name == "crate":
            dest_engine.execute(f"REFRESH TABLE {schema_rand_prefix}.output")
        results = dest_engine.execute(
            f"select id, val, updated_at from {schema_rand_prefix}.output order by id asc"
        ).fetchall()
        dest_engine.dispose()
        return results

    run()

    res = get_output_table()
    assert len(res) == 2

    # Compensate for CrateDB types and insert order.
    if dest_connection_url.startswith("cratedb://"):
        assert (1, "val1", 1640995200000) in res
        assert (2, "val2", 1641081600000) in res
    else:
        assert res[0] == (1, "val1", as_datetime("2022-01-01"))
        assert res[1] == (2, "val2", as_datetime("2022-01-02"))

    # # run again, nothing should be inserted into the output table
    run()

    res = get_output_table()
    assert len(res) == 2

    # Compensate for CrateDB types and insert order.
    if dest_connection_url.startswith("cratedb://"):
        assert (1, "val1", 1640995200000) in res
        assert (2, "val2", 1641081600000) in res
    else:
        assert res[0] == (1, "val1", as_datetime("2022-01-01"))
        assert res[1] == (2, "val2", as_datetime("2022-01-02"))


def db_to_db_merge_with_primary_key(
    source_connection_url: str,
    dest_connection_url: str,
    dest_connection_url_read: str,
):
    schema_rand_prefix = f"testschema_merge_{get_random_string(5)}"
    try:
        shutil.rmtree(get_abs_path("../pipeline_data"))
    except Exception:
        pass

    # CrateDB: Compensate for "Type `date` does not support storage".
    updated_at_type = "DATE"
    if dest_connection_url.startswith("cratedb://"):
        updated_at_type = "TIMESTAMP"

    source_engine = sqlalchemy.create_engine(source_connection_url)
    with source_engine.begin() as conn:
        conn.execute(f"DROP SCHEMA IF EXISTS {schema_rand_prefix}")
        conn.execute(f"CREATE SCHEMA {schema_rand_prefix}")
        conn.execute(
            f"CREATE TABLE {schema_rand_prefix}.input (id INTEGER NOT NULL, val VARCHAR(20), updated_at {updated_at_type} NOT NULL)"
        )
        conn.execute(
            f"INSERT INTO {schema_rand_prefix}.input VALUES (1, 'val1', '2022-01-01')"
        )
        conn.execute(
            f"INSERT INTO {schema_rand_prefix}.input VALUES (2, 'val2', '2022-02-01')"
        )

        res = conn.execute(
            f"select count(*) from {schema_rand_prefix}.input"
        ).fetchall()
        assert res[0][0] == 2

    source_engine.dispose()

    def run():
        res = invoke_ingest_command(
            source_connection_url,
            f"{schema_rand_prefix}.input",
            dest_connection_url,
            f"{schema_rand_prefix}.output",
            "merge",
            "updated_at",
            "id",
            sql_backend="sqlalchemy",
        )
        assert res.exit_code == 0
        return res

    dest_engine = sqlalchemy.create_engine(dest_connection_url_read)

    def get_output_rows():
        # CrateDB needs an explicit flush to make data available for reads immediately.
        if dest_engine.dialect.name == "crate":
            dest_engine.execute(f"REFRESH TABLE {schema_rand_prefix}.output")
        return dest_engine.execute(
            f"select id, val, updated_at from {schema_rand_prefix}.output order by id asc"
        ).fetchall()

    def assert_output_equals(expected):
        res = get_output_rows()
        assert len(res) == len(expected)
        for i, row in enumerate(expected):
            assert res[i] == row

    dest_engine.dispose()
    res = run()

    # Compensate for CrateDB types and insert order.
    if dest_connection_url.startswith("cratedb://"):
        assert_output_equals([(1, "val1", 1640995200000), (2, "val2", 1643673600000)])
    else:
        assert_output_equals(
            [
                (1, "val1", as_datetime("2022-01-01")),
                (2, "val2", as_datetime("2022-02-01")),
            ]
        )

    first_run_id = dest_engine.execute(
        f"select _dlt_load_id from {schema_rand_prefix}.output limit 1"
    ).fetchall()[0][0]

    dest_engine.dispose()

    ##############################
    # we'll run again, we don't expect any changes since the data hasn't changed
    res = run()
    if dest_connection_url.startswith("cratedb://"):
        assert_output_equals([(1, "val1", 1640995200000), (2, "val2", 1643673600000)])
    else:
        assert_output_equals(
            [
                (1, "val1", as_datetime("2022-01-01")),
                (2, "val2", as_datetime("2022-02-01")),
            ]
        )

    # we also ensure that the other rows were not touched
    count_by_run_id = dest_engine.execute(
        f"select _dlt_load_id, count(*) from {schema_rand_prefix}.output group by 1 order by 2 desc"
    ).fetchall()
    assert len(count_by_run_id) == 1
    assert count_by_run_id[0][1] == 2
    assert count_by_run_id[0][0] == first_run_id
    dest_engine.dispose()
    ##############################

    ##############################
    # now we'll modify the source data but not the updated at, the output table should not be updated
    source_engine.execute(
        f"UPDATE {schema_rand_prefix}.input SET val = 'val1_modified' WHERE id = 2"
    )
    source_engine.dispose()

    run()
    if dest_connection_url.startswith("cratedb://"):
        assert_output_equals([(1, "val1", 1640995200000), (2, "val2", 1643673600000)])
    else:
        assert_output_equals(
            [
                (1, "val1", as_datetime("2022-01-01")),
                (2, "val2", as_datetime("2022-02-01")),
            ]
        )

    # we also ensure that the other rows were not touched
    count_by_run_id = dest_engine.execute(
        f"select _dlt_load_id, count(*) from {schema_rand_prefix}.output group by 1"
    ).fetchall()
    assert len(count_by_run_id) == 1
    assert count_by_run_id[0][1] == 2
    assert count_by_run_id[0][0] == first_run_id
    dest_engine.dispose()
    ##############################

    ##############################
    # now we'll insert a new row but with an old date, the new row will not show up
    source_engine.execute(
        f"INSERT INTO {schema_rand_prefix}.input VALUES (3, 'val3', '2022-01-01')"
    )
    source_engine.dispose()

    run()
    if dest_connection_url.startswith("cratedb://"):
        assert_output_equals([(1, "val1", 1640995200000), (2, "val2", 1643673600000)])
    else:
        assert_output_equals(
            [
                (1, "val1", as_datetime("2022-01-01")),
                (2, "val2", as_datetime("2022-02-01")),
            ]
        )

    # we also ensure that the other rows were not touched
    count_by_run_id = dest_engine.execute(
        f"select _dlt_load_id, count(*) from {schema_rand_prefix}.output group by 1"
    ).fetchall()
    assert len(count_by_run_id) == 1
    assert count_by_run_id[0][1] == 2
    assert count_by_run_id[0][0] == first_run_id
    dest_engine.dispose()
    ##############################

    ##############################
    # now we'll insert a new row but with a new date, the new row will show up
    source_engine.execute(
        f"INSERT INTO {schema_rand_prefix}.input VALUES (3, 'val3', '2022-02-02')"
    )
    source_engine.dispose()

    run()
    if dest_connection_url.startswith("cratedb://"):
        assert_output_equals(
            [
                (1, "val1", 1640995200000),
                (2, "val2", 1643673600000),
                (3, "val3", 1643673600000),
            ]
        )
    else:
        assert_output_equals(
            [
                (1, "val1", as_datetime("2022-01-01")),
                (2, "val2", as_datetime("2022-02-01")),
                (3, "val3", as_datetime("2022-02-02")),
            ]
        )

    # we have a new run that inserted rows to this table, so the run count should be 2
    count_by_run_id = dest_engine.execute(
        f"select _dlt_load_id, count(*) from {schema_rand_prefix}.output group by 1 order by 2 desc"
    ).fetchall()
    assert len(count_by_run_id) == 2
    assert count_by_run_id[0][1] == 2
    assert count_by_run_id[0][0] == first_run_id
    # we don't care about the run ID
    assert count_by_run_id[1][1] == 1
    dest_engine.dispose()
    ##############################

    ##############################
    # lastly, let's try modifying the updated_at of an old column, it should be updated in the output table
    source_engine.execute(
        f"UPDATE {schema_rand_prefix}.input SET val='val2_modified', updated_at = '2022-02-03' WHERE id = 2"
    )
    source_engine.dispose()

    run()
    if dest_connection_url.startswith("cratedb://"):
        assert_output_equals(
            [
                (1, "val1", 1640995200000),
                (2, "val2_modified", 1643673600000),
                (3, "val3", 1643673600000),
            ]
        )
    else:
        assert_output_equals(
            [
                (1, "val1", as_datetime("2022-01-01")),
                (2, "val2_modified", as_datetime("2022-02-03")),
                (3, "val3", as_datetime("2022-02-02")),
            ]
        )

    # we have a new run that inserted rows to this table, so the run count should be 2
    count_by_run_id = dest_engine.execute(
        f"select _dlt_load_id, count(*) from {schema_rand_prefix}.output group by 1 order by 2 desc, 1 asc"
    ).fetchall()
    assert len(count_by_run_id) == 3
    assert count_by_run_id[0][1] == 1
    assert count_by_run_id[0][0] == first_run_id
    # we don't care about the rest of the run IDs
    assert count_by_run_id[1][1] == 1
    assert count_by_run_id[2][1] == 1
    dest_engine.dispose()
    ##############################


def db_to_db_delete_insert_without_primary_key(
    source_connection_url: str,
    dest_connection_url: str,
    dest_connection_url_read: str,
):
    schema_rand_prefix = f"testschema_delete_insert_{get_random_string(5)}"
    try:
        shutil.rmtree(get_abs_path("../pipeline_data"))
    except Exception:
        pass

    # CrateDB: Compensate for "Type `date` does not support storage".
    updated_at_type = "DATE"
    if dest_connection_url.startswith("cratedb://"):
        updated_at_type = "TIMESTAMP"

    source_engine = sqlalchemy.create_engine(source_connection_url)
    with source_engine.begin() as conn:
        conn.execute(f"DROP SCHEMA IF EXISTS {schema_rand_prefix}")
        conn.execute(f"CREATE SCHEMA {schema_rand_prefix}")
        conn.execute(
            f"CREATE TABLE {schema_rand_prefix}.input (id INTEGER, val VARCHAR(20), updated_at {updated_at_type})"
        )
        conn.execute(
            f"INSERT INTO {schema_rand_prefix}.input VALUES (1, 'val1', '2022-01-01')"
        )
        conn.execute(
            f"INSERT INTO {schema_rand_prefix}.input VALUES (2, 'val2', '2022-02-01')"
        )

        res = conn.execute(
            f"select count(*) from {schema_rand_prefix}.input"
        ).fetchall()
        assert res[0][0] == 2
    source_engine.dispose()

    def run():
        res = invoke_ingest_command(
            source_connection_url,
            f"{schema_rand_prefix}.input",
            dest_connection_url,
            f"{schema_rand_prefix}.output",
            inc_strategy="delete+insert",
            inc_key="updated_at",
            sql_backend="sqlalchemy",
        )
        if res.exit_code != 0:
            traceback.print_exception(*res.exc_info)
        assert res.exit_code == 0
        return res

    dest_engine = sqlalchemy.create_engine(dest_connection_url_read)

    def get_output_rows():
        # CrateDB needs an explicit flush to make data available for reads immediately.
        if dest_engine.dialect.name == "crate":
            dest_engine.execute(f"REFRESH TABLE {schema_rand_prefix}.output")
        results = dest_engine.execute(
            f"select id, val, updated_at from {schema_rand_prefix}.output order by id asc"
        ).fetchall()
        dest_engine.dispose()
        return results

    def assert_output_equals(expected):
        res = get_output_rows()
        assert len(res) == len(expected)
        for i, row in enumerate(expected):
            assert res[i] == row

    run()
    if dest_connection_url.startswith("cratedb://"):
        assert_output_equals([(1, "val1", 1640995200000), (2, "val2", 1643673600000)])
    else:
        assert_output_equals(
            [
                (1, "val1", as_datetime("2022-01-01")),
                (2, "val2", as_datetime("2022-02-01")),
            ]
        )

    first_run_id = dest_engine.execute(
        f"select _dlt_load_id from {schema_rand_prefix}.output limit 1"
    ).fetchall()[0][0]
    dest_engine.dispose()

    ##############################
    # we'll run again, since this is a delete+insert, we expect the run ID to change for the last one
    res = run()
    if dest_connection_url.startswith("cratedb://"):
        assert_output_equals([(1, "val1", 1640995200000), (2, "val2", 1643673600000)])
    else:
        assert_output_equals(
            [
                (1, "val1", as_datetime("2022-01-01")),
                (2, "val2", as_datetime("2022-02-01")),
            ]
        )

    # we ensure that one of the rows is updated with a new run
    count_by_run_id = dest_engine.execute(
        f"select _dlt_load_id, count(*) from {schema_rand_prefix}.output group by 1 order by 1 asc"
    ).fetchall()
    assert len(count_by_run_id) == 2
    assert count_by_run_id[0][0] == first_run_id
    assert count_by_run_id[0][1] == 1
    assert count_by_run_id[1][0] != first_run_id
    assert count_by_run_id[1][1] == 1
    dest_engine.dispose()
    ##############################

    ##############################
    # now we'll insert a few more lines for the same day, the new rows should show up
    source_engine.execute(
        f"INSERT INTO {schema_rand_prefix}.input VALUES (3, 'val3', '2022-02-01'), (4, 'val4', '2022-02-01')"
    )
    source_engine.dispose()

    run()
    if dest_connection_url.startswith("cratedb://"):
        assert_output_equals(
            [
                (1, "val1", 1640995200000),
                (2, "val2", 1643673600000),
                (3, "val3", 1643673600000),
                (4, "val4", 1643673600000),
            ]
        )
    else:
        assert_output_equals(
            [
                (1, "val1", as_datetime("2022-01-01")),
                (2, "val2", as_datetime("2022-02-01")),
                (3, "val3", as_datetime("2022-02-01")),
                (4, "val4", as_datetime("2022-02-01")),
            ]
        )

    # the new rows should have a new run ID, there should be 2 distinct runs now
    count_by_run_id = dest_engine.execute(
        f"select _dlt_load_id, count(*) from {schema_rand_prefix}.output group by 1 order by 2 desc, 1 asc"
    ).fetchall()
    assert len(count_by_run_id) == 2
    assert count_by_run_id[0][0] != first_run_id
    assert count_by_run_id[0][1] == 3  # 2 new rows + 1 old row
    assert count_by_run_id[1][0] == first_run_id
    assert count_by_run_id[1][1] == 1
    dest_engine.dispose()
    ##############################


def db_to_db_delete_insert_with_timerange(
    source_connection_url: str,
    dest_connection_url: str,
    dest_connection_url_read: str,
):
    schema_rand_prefix = f"testschema_delete_insert_timerange_{get_random_string(5)}"
    source_engine = sqlalchemy.create_engine(source_connection_url)

    source_engine.execute(f"DROP SCHEMA IF EXISTS {schema_rand_prefix}")
    source_engine.execute(f"CREATE SCHEMA {schema_rand_prefix}")
    try:
        source_engine.execute(
            f"CREATE TABLE {schema_rand_prefix}.input (id INTEGER, val VARCHAR(20), updated_at DATETIME)"
        )
    except Exception:
        # hello postgres
        source_engine.execute(
            f"CREATE TABLE {schema_rand_prefix}.input (id INTEGER, val VARCHAR(20), updated_at TIMESTAMP)"
        )

    source_engine.execute(
        f"""INSERT INTO {schema_rand_prefix}.input VALUES 
        (1, 'val1', '2022-01-01T00:00:00'),
        (2, 'val2', '2022-01-01T00:00:00'),
        (3, 'val3', '2022-01-02T00:00:00'),
        (4, 'val4', '2022-01-02T00:00:00'),
        (5, 'val5', '2022-01-03T00:00:00'),
        (6, 'val6', '2022-01-03T00:00:00')
    """
    )

    res = source_engine.execute(
        f"select count(*) from {schema_rand_prefix}.input"
    ).fetchall()
    assert res[0][0] == 6
    source_engine.dispose()

    def run(start_date: str, end_date: str):
        res = invoke_ingest_command(
            source_connection_url,
            f"{schema_rand_prefix}.input",
            dest_connection_url,
            f"{schema_rand_prefix}.output",
            inc_strategy="delete+insert",
            inc_key="updated_at",
            interval_start=start_date,
            interval_end=end_date,
            sql_backend="sqlalchemy",
        )
        assert res.exit_code == 0
        return res

    dest_engine = sqlalchemy.create_engine(dest_connection_url_read, poolclass=NullPool)

    def get_output_rows():
        # CrateDB needs an explicit flush to make data available for reads immediately.
        if dest_engine.dialect.name == "crate":
            dest_engine.execute(f"REFRESH TABLE {schema_rand_prefix}.output")
        if "clickhouse" not in dest_connection_url:
            dest_engine.execute("CHECKPOINT")
        rows = dest_engine.execute(
            f"select id, val, updated_at from {schema_rand_prefix}.output order by id asc"
        ).fetchall()
        return [(row[0], row[1], row[2].date()) for row in rows]

    def assert_output_equals(expected):
        res = get_output_rows()
        assert len(res) == len(expected)
        for i, row in enumerate(expected):
            assert res[i] == row

    run("2022-01-01", "2022-01-02")  # dlt runs them with the end date exclusive
    if dest_connection_url.startswith("cratedb://"):
        assert_output_equals(
            [
                (1, "val1", 1640995200000),
                (2, "val2", 1640995200000),
                (3, "val3", 1643673600000),
                (4, "val4", 1643673600000),
            ]
        )
    else:
        assert_output_equals(
            [
                (1, "val1", as_datetime("2022-01-01")),
                (2, "val2", as_datetime("2022-01-01")),
                (3, "val3", as_datetime("2022-01-02")),
                (4, "val4", as_datetime("2022-01-02")),
            ]
        )

    first_run_id = dest_engine.execute(
        f"select _dlt_load_id from {schema_rand_prefix}.output limit 1"
    ).fetchall()[0][0]
    dest_engine.dispose()

    ##############################
    # we'll run again, since this is a delete+insert, we expect the run ID to change for the last one
    res = run("2022-01-01", "2022-01-02")

    assert_output_equals(
        [
            (1, "val1", as_datetime("2022-01-01")),
            (2, "val2", as_datetime("2022-01-01")),
            (3, "val3", as_datetime("2022-01-02")),
            (4, "val4", as_datetime("2022-01-02")),
        ]
    )

    # both rows should have a new run ID
    count_by_run_id = dest_engine.execute(
        f"select _dlt_load_id, count(*) from {schema_rand_prefix}.output group by 1 order by 1 asc"
    ).fetchall()
    assert len(count_by_run_id) == 1
    assert count_by_run_id[0][0] != first_run_id
    assert count_by_run_id[0][1] == 4
    dest_engine.dispose()
    ##############################

    ##############################
    # now run for the day after, new rows should land
    run("2022-01-02", "2022-01-03")
    assert_output_equals(
        [
            (1, "val1", as_datetime("2022-01-01")),
            (2, "val2", as_datetime("2022-01-01")),
            (3, "val3", as_datetime("2022-01-02")),
            (4, "val4", as_datetime("2022-01-02")),
            (5, "val5", as_datetime("2022-01-03")),
            (6, "val6", as_datetime("2022-01-03")),
        ]
    )

    # there should be 4 rows with 2 distinct run IDs
    count_by_run_id = dest_engine.execute(
        f"select _dlt_load_id, count(*) from {schema_rand_prefix}.output group by 1 order by 1 asc"
    ).fetchall()
    assert len(count_by_run_id) == 2
    assert count_by_run_id[0][1] == 2
    assert count_by_run_id[1][1] == 4
    dest_engine.dispose()
    ##############################

    ##############################
    # let's bring in the rows for the third day
    run("2022-01-03", "2022-01-04")
    assert_output_equals(
        [
            (1, "val1", as_datetime("2022-01-01")),
            (2, "val2", as_datetime("2022-01-01")),
            (3, "val3", as_datetime("2022-01-02")),
            (4, "val4", as_datetime("2022-01-02")),
            (5, "val5", as_datetime("2022-01-03")),
            (6, "val6", as_datetime("2022-01-03")),
        ]
    )

    # there should be 6 rows with 3 distinct run IDs
    count_by_run_id = dest_engine.execute(
        f"select _dlt_load_id, count(*) from {schema_rand_prefix}.output group by 1 order by 1 asc"
    ).fetchall()
    assert len(count_by_run_id) == 3
    assert count_by_run_id[0][1] == 2
    assert count_by_run_id[1][1] == 2
    assert count_by_run_id[2][1] == 2
    dest_engine.dispose()
    ##############################

    ##############################
    # now let's do a backfill for the first day again, the rows should be updated
    source_engine.execute(
        f"UPDATE {schema_rand_prefix}.input SET val = 'val1_modified' WHERE id = 1"
    )
    source_engine.dispose()

    run("2022-01-01", "2022-01-02")
    assert_output_equals(
        [
            (1, "val1_modified", as_datetime("2022-01-01")),
            (2, "val2", as_datetime("2022-01-01")),
            (3, "val3", as_datetime("2022-01-02")),
            (4, "val4", as_datetime("2022-01-02")),
            (5, "val5", as_datetime("2022-01-03")),
            (6, "val6", as_datetime("2022-01-03")),
        ]
    )

    # there should still be 6 rows with 3 distinct run IDs
    count_by_run_id = dest_engine.execute(
        f"select _dlt_load_id, count(*) from {schema_rand_prefix}.output group by 1 order by 1 asc"
    ).fetchall()
    assert len(count_by_run_id) == 2
    assert count_by_run_id[0][1] == 2
    assert count_by_run_id[1][1] == 4
    dest_engine.dispose()
    ##############################


def as_datetime(date_str: str) -> date:
    return datetime.strptime(date_str, "%Y-%m-%d").replace(tzinfo=timezone.utc).date()


def as_datetime2(date_str: str) -> datetime:
    return datetime.strptime(date_str, "%Y-%m-%d")


@pytest.mark.parametrize(
    "dest", list(DESTINATIONS.values()), ids=list(DESTINATIONS.keys())
)
def test_kafka_to_db(dest):
    with ThreadPoolExecutor() as executor:
        dest_future = executor.submit(dest.start)
        source_future = executor.submit(
            KafkaContainer("confluentinc/cp-kafka:7.6.0").start, timeout=120
        )
        dest_uri = dest_future.result()
        kafka = source_future.result()

    # kafka = KafkaContainer("confluentinc/cp-kafka:7.6.0").start(timeout=60)

    # Create Kafka producer
    producer = Producer({"bootstrap.servers": kafka.get_bootstrap_server()})

    # Create topic and send messages
    topic = "test_topic"
    messages = ["message1", "message2", "message3"]

    for message in messages:
        producer.produce(topic, message.encode("utf-8"))
    producer.flush()

    def run():
        res = invoke_ingest_command(
            f"kafka://?bootstrap_servers={kafka.get_bootstrap_server()}&group_id=test_group",
            "test_topic",
            dest_uri,
            "testschema.output",
        )
        assert res.exit_code == 0

    def get_output_table():
        dest_uri_read = get_uri_read(dest_uri, dest)
        dest_engine = sqlalchemy.create_engine(dest_uri_read)
        # CrateDB needs an explicit flush to make data available for reads immediately.
        if dest_engine.dialect.name == "crate":
            dest_engine.execute("REFRESH TABLE testschema.output")
        with dest_engine.connect() as conn:
            res = conn.execute(
                "select _kafka__data from testschema.output order by _kafka__msg_id asc"
            ).fetchall()
        dest_engine.dispose()
        return res

    run()

    res = get_output_table()
    assert len(res) == 3
    if dest_uri.startswith("cratedb://"):
        messages_db = [res[0][0], res[1][0], res[2][0]]
        assert "message1" in messages_db
        assert "message2" in messages_db
        assert "message3" in messages_db
    else:
        assert res[0] == ("message1",)
        assert res[1] == ("message2",)
        assert res[2] == ("message3",)

    # run again, nothing should be inserted into the output table
    run()

    res = get_output_table()
    assert len(res) == 3
    if dest_uri.startswith("cratedb://"):
        messages_db = [res[0][0], res[1][0], res[2][0]]
        assert "message1" in messages_db
        assert "message2" in messages_db
        assert "message3" in messages_db
    else:
        assert res[0] == ("message1",)
        assert res[1] == ("message2",)
        assert res[2] == ("message3",)

    # add a new message
    producer.produce(topic, "message4".encode("utf-8"))
    producer.flush()

    # run again, the new message should be inserted into the output table
    run()
    res = get_output_table()
    assert len(res) == 4
    if dest_uri.startswith("cratedb://"):
        messages_db = [res[0][0], res[1][0], res[2][0], res[3][0]]
        assert "message1" in messages_db
        assert "message2" in messages_db
        assert "message3" in messages_db
        assert "message4" in messages_db
    else:
        assert res[0] == ("message1",)
        assert res[1] == ("message2",)
        assert res[2] == ("message3",)
        assert res[3] == ("message4",)

    kafka.stop()


@pytest.mark.parametrize(
    "dest", list(DESTINATIONS.values()), ids=list(DESTINATIONS.keys())
)
def test_arrow_mmap_to_db_create_replace(dest):
    if isinstance(dest, CrateDbDockerImage):
        pytest.skip(
            "CrateDB type mapping does not support `DATE` yet, "
            "see https://github.com/crate-workbench/ingestr/issues/4"
        )

    schema = f"testschema_arrow_mmap_create_replace_{get_random_string(5)}"

    def run_command(
        table: pa.Table,
        incremental_key: Optional[str] = None,
        incremental_strategy: Optional[str] = None,
    ):
        with tempfile.NamedTemporaryFile(suffix=".arrow", delete=True) as tmp:
            with pa.OSFile(tmp.name, "wb") as f:
                writer = ipc.new_file(f, table.schema)
                writer.write_table(table)
                writer.close()

            res = invoke_ingest_command(
                f"mmap://{tmp.name}",
                "whatever",
                dest_uri,
                f"{schema}.output",
                # we use this because postgres destination fails with nested fields, gonna have to investigate this more
                loader_file_format=(
                    "insert_values" if dest_uri.startswith("postgresql") else None
                ),
            )

            assert res.exit_code == 0
            return res

    dest_uri = dest.start()

    # let's start with a basic dataframe
    row_count = 1000
    df = pd.DataFrame(
        {
            "id": range(row_count),
            "value": np.random.rand(row_count),
            "category": np.random.choice(["A", "B", "C"], size=row_count),
            "nested": [{"a": 1, "b": 2, "c": {"d": 3}}] * row_count,
            "date": [as_datetime("2024-11-05")] * row_count,
        }
    )

    table = pa.Table.from_pandas(df)
    run_command(table)

    dest_uri_read = get_uri_read(dest_uri, dest)
    dest_engine = sqlalchemy.create_engine(dest_uri_read)
    with dest_engine.begin() as conn:
        res = conn.execute(f"select count(*) from {schema}.output").fetchall()
        assert res[0][0] == row_count

        res = conn.execute(
            f"select date, count(*) from {schema}.output group by 1 order by 1 asc"
        ).fetchall()
        assert res[0][0] == as_datetime("2024-11-05")
        assert res[0][1] == row_count
    dest_engine.dispose()

    # let's add a new column to the dataframe
    df["new_col"] = "some value"
    table = pa.Table.from_pandas(df)
    run_command(table)

    # there should be no change, just a new column
    with dest_engine.begin() as conn:
        res = conn.execute(f"select count(*) from {schema}.output").fetchall()
        assert res[0][0] == row_count

        res = conn.execute(
            f"select date, count(*) from {schema}.output group by 1 order by 1 asc"
        ).fetchall()
        assert res[0][0] == as_datetime("2024-11-05")
        assert res[0][1] == row_count

        res = conn.execute(
            f"select new_col, count(*) from {schema}.output group by 1 order by 1 asc"
        ).fetchall()
        assert res[0][0] == "some value"
        assert res[0][1] == row_count
    dest_engine.dispose()


@pytest.mark.parametrize(
    "dest", list(DESTINATIONS.values()), ids=list(DESTINATIONS.keys())
)
def test_arrow_mmap_to_db_delete_insert(dest):
    schema = f"testschema_arrow_mmap_del_ins_{get_random_string(5)}"

    def run_command(df: pd.DataFrame, incremental_key: Optional[str] = None):
        table = pa.Table.from_pandas(df)
        with tempfile.NamedTemporaryFile(suffix=".arrow", delete=True) as tmp:
            with pa.OSFile(tmp.name, "wb") as f:
                writer = ipc.new_file(f, table.schema)
                writer.write_table(table)
                writer.close()

            res = invoke_ingest_command(
                f"mmap://{tmp.name}",
                "whatever",
                dest_uri,
                f"{schema}.output",
                inc_key=incremental_key,
                inc_strategy="delete+insert",
            )

            assert res.exit_code == 0
            return res

    dest_uri = dest.start()
    if "clickhouse" in dest_uri:
        pytest.skip("clickhouse is not supported for this test")
    if dest_uri.startswith("cratedb://"):
        pytest.skip(
            "CrateDB type mapping does not support `DATE` yet, "
            "see https://github.com/crate-workbench/ingestr/issues/4"
        )

    dest_uri_read = get_uri_read(dest_uri, dest)
    dest_engine = sqlalchemy.create_engine(dest_uri_read)

    # let's start with a basic dataframe
    row_count = 1000
    df = pd.DataFrame(
        {
            "id": range(row_count),
            "value": np.random.rand(row_count),
            "category": np.random.choice(["A", "B", "C"], size=row_count),
            "date": pd.to_datetime(["2024-11-05"] * row_count),
        }
    )

    run_command(df, "date")

    def build_datetime(ds: str):
        dt: datetime = as_datetime2(ds)
        if dest_uri.startswith("clickhouse"):
            dt = dt.replace(tzinfo=timezone.utc)
        return dt

    def compare_dates(actual, expected_str):
        """Compare dates ignoring timezone for dlt 1.16.0 compatibility"""
        expected_date = build_datetime(expected_str)

        # If actual has timezone info and it causes time offset, compare just dates
        if hasattr(actual, "tzinfo") and actual.tzinfo is not None:
            # Compare only the date part for timezone-aware datetimes
            if hasattr(actual, "date") and hasattr(expected_date, "date"):
                return actual.date() == expected_date.date()

        # For timezone-naive comparison
        actual_date = actual
        if hasattr(actual_date, "replace"):
            actual_date = actual_date.replace(tzinfo=None)
        if hasattr(expected_date, "replace"):
            expected_date = expected_date.replace(tzinfo=None)
        return actual_date == expected_date

    # the first load, it should be loaded correctly
    with dest_engine.begin() as conn:
        res = conn.execute(f"select count(*) from {schema}.output").fetchall()
        assert res[0][0] == row_count

        res = conn.execute(
            f"select date, count(*) from {schema}.output group by 1 order by 1 asc"
        ).fetchall()
        assert compare_dates(res[0][0], "2024-11-05")
        assert res[0][1] == row_count

    dest_engine.dispose()

    # run again, it should be deleted and reloaded
    run_command(df, "date")
    with dest_engine.begin() as conn:
        res = conn.execute(f"select count(*) from {schema}.output").fetchall()
        assert res[0][0] == row_count

        res = conn.execute(
            f"select date, count(*) from {schema}.output group by 1 order by 1 asc"
        ).fetchall()
        assert compare_dates(res[0][0], "2024-11-05")
        assert res[0][1] == row_count
    dest_engine.dispose()

    # append 1000 new rows with a different date
    new_rows = pd.DataFrame(
        {
            "id": range(row_count, row_count + 1000),
            "value": np.random.rand(1000),
            "category": np.random.choice(["A", "B", "C"], size=1000),
            "date": pd.to_datetime(["2024-11-06"] * 1000),
        }
    )
    df = pd.concat([df, new_rows], ignore_index=True)

    run_command(df, "date")

    with dest_engine.begin() as conn:
        res = conn.execute(f"select count(*) from {schema}.output").fetchall()
        assert res[0][0] == row_count + 1000

        res = conn.execute(
            f"select date, count(*) from {schema}.output group by 1 order by 1 asc"
        ).fetchall()
        assert compare_dates(res[0][0], "2024-11-05")
        assert res[0][1] == row_count
        assert compare_dates(res[1][0], "2024-11-06")
        assert res[1][1] == 1000
    dest_engine.dispose()

    # append 1000 old rows for a previous date, these should not be loaded
    old_rows = pd.DataFrame(
        {
            "id": range(row_count, row_count + 1000),
            "value": np.random.rand(1000),
            "category": np.random.choice(["A", "B", "C"], size=1000),
            "date": pd.to_datetime(["2024-11-04"] * 1000),
        }
    )
    df = pd.concat([df, old_rows], ignore_index=True)

    run_command(df, "date")
    with dest_engine.begin() as conn:
        res = conn.execute(f"select count(*) from {schema}.output").fetchall()
        assert res[0][0] == row_count + 1000

        res = conn.execute(
            f"select date, count(*) from {schema}.output group by 1 order by 1 asc"
        ).fetchall()
        assert compare_dates(res[0][0], "2024-11-05")
        assert res[0][1] == row_count
        assert compare_dates(res[1][0], "2024-11-06")
        assert res[1][1] == 1000
    dest_engine.dispose()


@pytest.mark.parametrize(
    "dest", list(DESTINATIONS.values()), ids=list(DESTINATIONS.keys())
)
def test_arrow_mmap_to_db_merge_without_incremental(dest):
    if isinstance(dest, CrateDbDockerImage):
        pytest.skip(
            "CrateDB type mapping does not support `DATE` yet, "
            "see https://github.com/crate-workbench/ingestr/issues/4"
        )
    schema = f"testschema_arrow_mmap_{get_random_string(5)}"

    def run_command(df: pd.DataFrame):
        table = pa.Table.from_pandas(df)
        with tempfile.NamedTemporaryFile(suffix=".arrow", delete=True) as tmp:
            with pa.OSFile(tmp.name, "wb") as f:
                writer = ipc.new_file(f, table.schema)
                writer.write_table(table)
                writer.close()

            res = invoke_ingest_command(
                f"mmap://{tmp.name}",
                "whatever",
                dest_uri,
                f"{schema}.output",
                inc_strategy="merge",
                primary_key="id",
            )
            assert res.exit_code == 0
            return res

    dest_uri = dest.start()

    dest_uri_read = get_uri_read(dest_uri, dest)
    dest_engine = sqlalchemy.create_engine(dest_uri_read)

    # let's start with a basic dataframe
    row_count = 1000
    df = pd.DataFrame({"id": range(row_count), "value": ["a"] * row_count})

    run_command(df)

    # the first load, it should be loaded correctly
    with dest_engine.begin() as conn:
        res = conn.execute(f"select count(*) from {schema}.output").fetchall()
        assert res[0][0] == row_count

        res = conn.execute(
            f"select value, count(*) from {schema}.output group by 1 order by 1 asc"
        ).fetchall()
        assert res[0][0] == "a"
        assert res[0][1] == row_count
    dest_engine.dispose()

    # run again, no change
    run_command(df)
    with dest_engine.begin() as conn:
        res = conn.execute(f"select count(*) from {schema}.output").fetchall()
        assert res[0][0] == row_count

        res = conn.execute(
            f"select value, count(*) from {schema}.output group by 1 order by 1 asc"
        ).fetchall()
        assert res[0][0] == "a"
        assert res[0][1] == row_count
    dest_engine.dispose()

    # append 1000 new rows with a different value
    new_rows = pd.DataFrame(
        {
            "id": range(row_count, row_count + 1000),
            "value": ["b"] * 1000,
        }
    )
    df = pd.concat([df, new_rows], ignore_index=True)

    run_command(df)

    with dest_engine.begin() as conn:
        res = conn.execute(f"select count(*) from {schema}.output").fetchall()

        assert res[0][0] == row_count + 1000

        res = conn.execute(
            f"select value, count(*) from {schema}.output group by 1 order by 1 asc"
        ).fetchall()
        assert res[0][0] == "a"
        assert res[0][1] == row_count
        assert res[1][0] == "b"
        assert res[1][1] == 1000

    dest_engine.dispose()

    # append 1000 old rows for previous ids, they should be merged
    old_rows = pd.DataFrame(
        {
            "id": range(row_count, row_count + 1000),
            "value": ["a"] * 1000,
        }
    )
    run_command(old_rows)
    with dest_engine.begin() as conn:
        res = conn.execute(f"select count(*) from {schema}.output").fetchall()
        assert res[0][0] == row_count + 1000
        res = conn.execute(
            f"select value, count(*) from {schema}.output group by 1 order by 1 asc"
        ).fetchall()
        assert res[0][0] == "a"
        assert res[0][1] == row_count + 1000
    dest_engine.dispose()


@pytest.mark.parametrize(
    "dest", list(DESTINATIONS.values()), ids=list(DESTINATIONS.keys())
)
@pytest.mark.parametrize("source", list(SOURCES.values()), ids=list(SOURCES.keys()))
def test_db_to_db_exclude_columns(source, dest):
    if isinstance(source.container, SqlServerContainer) and isinstance(
        dest, CrateDbDockerImage
    ):
        pytest.skip(
            "CrateDB type mapping does not support `DATE` yet, "
            "see https://github.com/crate-workbench/ingestr/issues/4"
        )
    with ThreadPoolExecutor() as executor:
        source_future = executor.submit(source.start)
        dest_future = executor.submit(dest.start)
        source_uri = source_future.result()
        dest_uri = dest_future.result()

    schema_rand_prefix = f"testschema_db_to_db_exclude_columns_{get_random_string(5)}"

    # CrateDB: Compensate for "Type `date` does not support storage".
    updated_at_type = "DATE"
    if dest_uri.startswith("cratedb://"):
        updated_at_type = "TIMESTAMP"

    source_engine = sqlalchemy.create_engine(source_uri)
    with source_engine.begin() as conn:
        conn.execute(f"DROP SCHEMA IF EXISTS {schema_rand_prefix}")
        conn.execute(f"CREATE SCHEMA {schema_rand_prefix}")
        conn.execute(
            f"CREATE TABLE {schema_rand_prefix}.input (id INTEGER, val VARCHAR(20), updated_at {updated_at_type}, col_to_exclude1 VARCHAR(20), col_to_exclude2 VARCHAR(20))"
        )
        conn.execute(
            f"INSERT INTO {schema_rand_prefix}.input VALUES (1, 'val1', '2022-01-01', 'col1', 'col2')"
        )
        conn.execute(
            f"INSERT INTO {schema_rand_prefix}.input VALUES (2, 'val2', '2022-02-01', 'col1', 'col2')"
        )
        res = conn.execute(
            f"select count(*) from {schema_rand_prefix}.input"
        ).fetchall()
        assert res[0][0] == 2
    source_engine.dispose()
    result = invoke_ingest_command(
        source_uri,
        f"{schema_rand_prefix}.input",
        dest_uri,
        f"{schema_rand_prefix}.output",
        sql_exclude_columns="col_to_exclude1,col_to_exclude2",
    )

    assert result.exit_code == 0

    dest_uri_read = get_uri_read(dest_uri, dest)
    dest_engine = sqlalchemy.create_engine(dest_uri_read)
    # CrateDB needs an explicit flush to make data available for reads immediately.
    if dest_engine.dialect.name == "crate":
        dest_engine.execute(f"REFRESH TABLE {schema_rand_prefix}.output")
    res = dest_engine.execute(
        f"select id, val, updated_at from {schema_rand_prefix}.output"
    ).fetchall()

    assert len(res) == 2
    if dest_uri.startswith("cratedb://"):
        assert (1, "val1", 1640995200000) in res
        assert (2, "val2", 1643673600000) in res
    else:
        assert res[0] == (1, "val1", as_datetime("2022-01-01"))
        assert res[1] == (2, "val2", as_datetime("2022-02-01"))

    # Verify excluded columns don't exist in destination schema
    columns = dest_engine.execute(
        f"SELECT column_name FROM information_schema.columns WHERE table_schema = '{schema_rand_prefix}' AND table_name = 'output'"
    ).fetchall()
    assert columns == [("id",), ("val",), ("updated_at",)]

    # Clean up
    dest_engine.dispose()
    source.stop()
    dest.stop()


def test_sql_limit():
    source_instance = EphemeralDuckDb()
    dest_instance = EphemeralDuckDb()

    source_uri = source_instance.start()
    dest_uri = dest_instance.start()

    schema_rand_prefix = f"test_sql_limit_{get_random_string(5)}"
    source_engine = sqlalchemy.create_engine(source_uri, poolclass=NullPool)
    with source_engine.begin() as conn:
        conn.execute(f"DROP SCHEMA IF EXISTS {schema_rand_prefix}")
        conn.execute(f"CREATE SCHEMA {schema_rand_prefix}")
        conn.execute(
            f"CREATE TABLE {schema_rand_prefix}.input (id INTEGER, val VARCHAR(20), updated_at DATE)"
        )
        conn.execute(
            f"""INSERT INTO {schema_rand_prefix}.input VALUES 
                (1, 'val1', '2024-01-01'),
                (2, 'val2', '2024-01-01'),
                (3, 'val3', '2024-01-01'),
                (4, 'val4', '2024-01-02'),
                (5, 'val5', '2024-01-02')"""
        )
        res = conn.execute(
            f"select count(*) from {schema_rand_prefix}.input"
        ).fetchall()
        assert res[0][0] == 5

    result = invoke_ingest_command(
        source_uri,
        f"{schema_rand_prefix}.input",
        dest_uri,
        f"{schema_rand_prefix}.output",
        sql_backend="sqlalchemy",
        sql_limit=4,
    )
    if result.exception:
        traceback.print_exception(*result.exc_info)
    assert result.exit_code == 0

    dest_engine = sqlalchemy.create_engine(dest_uri, poolclass=NullPool)
    res = dest_engine.execute(
        f"select id, val, updated_at from {schema_rand_prefix}.output order by id asc"
    ).fetchall()

    assert res == [
        (1, "val1", as_datetime("2024-01-01")),
        (2, "val2", as_datetime("2024-01-01")),
        (3, "val3", as_datetime("2024-01-01")),
        (4, "val4", as_datetime("2024-01-02")),
    ]

    source_instance.stop()
    dest_instance.stop()


def test_date_coercion_issue():
    """
    By default, ingestr treats the start and end dates as datetime objects. While this worked fine for many cases, if the
    incremental field is a date, the start and end dates cannot be compared to the incremental field, and the ingestion would fail.
    In order to eliminate this, we have introduced a new option to ingestr, --columns, which allows the user to specify the column types for the destination table.
    This way, ingestr will know the data type of the incremental field, and will be able to convert the start and end dates to the correct data type before running the ingestion.
    """
    source_instance = EphemeralDuckDb()
    dest_instance = EphemeralDuckDb()

    source_uri = source_instance.start()
    dest_uri = dest_instance.start()

    schema_rand_prefix = f"test_date_coercion_{get_random_string(5)}"
    source_engine = sqlalchemy.create_engine(source_uri, poolclass=NullPool)
    with source_engine.begin() as conn:
        conn.execute(f"DROP SCHEMA IF EXISTS {schema_rand_prefix}")
        conn.execute(f"CREATE SCHEMA {schema_rand_prefix}")
        conn.execute(
            f"CREATE TABLE {schema_rand_prefix}.input (id INTEGER, val VARCHAR(20), updated_at DATE)"
        )
        conn.execute(
            f"""INSERT INTO {schema_rand_prefix}.input VALUES 
                (1, 'val1', '2024-01-01'),
                (2, 'val2', '2024-01-01'),
                (3, 'val3', '2024-01-01'),
                (4, 'val4', '2024-01-02'),
                (5, 'val5', '2024-01-02'),
                (6, 'val6', '2024-01-02'),
                (7, 'val7', '2024-01-03'),
                (8, 'val8', '2024-01-03'),
                (9, 'val9', '2024-01-03')"""
        )
        res = conn.execute(
            f"select count(*) from {schema_rand_prefix}.input"
        ).fetchall()
        assert res[0][0] == 9

    result = invoke_ingest_command(
        source_uri,
        f"{schema_rand_prefix}.input",
        dest_uri,
        f"{schema_rand_prefix}.output",
        inc_strategy="delete+insert",
        inc_key="updated_at",
        sql_backend="sqlalchemy",
        interval_start="2024-01-01",
        interval_end="2024-01-02",
        columns="updated_at:date",
    )
    if result.exception:
        traceback.print_exception(*result.exc_info)
    assert result.exit_code == 0

    dest_engine = sqlalchemy.create_engine(dest_uri, poolclass=NullPool)
    res = dest_engine.execute(
        f"select id, val, updated_at from {schema_rand_prefix}.output order by id asc"
    ).fetchall()

    assert res == [
        (1, "val1", as_datetime("2024-01-01")),
        (2, "val2", as_datetime("2024-01-01")),
        (3, "val3", as_datetime("2024-01-01")),
        (4, "val4", as_datetime("2024-01-02")),
        (5, "val5", as_datetime("2024-01-02")),
        (6, "val6", as_datetime("2024-01-02")),
    ]

    source_instance.stop()
    dest_instance.stop()


def test_duckdb_masking_basic():
    """
    Test basic masking functionality with DuckDB source and destination.
    Tests hash, partial, redact, and round masking algorithms.
    """
    source_instance = EphemeralDuckDb()
    dest_instance = EphemeralDuckDb()

    source_uri = source_instance.start()
    dest_uri = dest_instance.start()

    schema_rand_prefix = f"test_masking_{get_random_string(5)}"
    source_engine = sqlalchemy.create_engine(source_uri, poolclass=NullPool)

    # Create test data with sensitive information
    with source_engine.begin() as conn:
        conn.execute(f"DROP SCHEMA IF EXISTS {schema_rand_prefix}")
        conn.execute(f"CREATE SCHEMA {schema_rand_prefix}")
        conn.execute(
            f"""CREATE TABLE {schema_rand_prefix}.customers (
                id INTEGER,
                name VARCHAR(100),
                email VARCHAR(100),
                phone VARCHAR(20),
                ssn VARCHAR(15),
                salary INTEGER,
                created_date DATE
            )"""
        )
        conn.execute(
            f"""INSERT INTO {schema_rand_prefix}.customers VALUES 
                (1, 'John Doe', 'john.doe@example.com', '555-123-4567', '123-45-6789', 52300, '2024-01-15'),
                (2, 'Jane Smith', 'jane.smith@gmail.com', '555-987-6543', '987-65-4321', 67800, '2024-02-20'),
                (3, 'Bob Johnson', 'bob.j@company.org', '555-555-1234', '456-78-9012', 45000, '2024-03-10')
            """
        )

    # Run ingestion with masking
    result = invoke_ingest_command(
        source_uri,
        f"{schema_rand_prefix}.customers",
        dest_uri,
        f"{schema_rand_prefix}.masked_customers",
        mask=["email:hash", "phone:partial:3", "ssn:redact", "salary:round:5000"],
    )

    assert result.exit_code == 0

    # Verify masked data
    dest_engine = sqlalchemy.create_engine(dest_uri, poolclass=NullPool)
    res = dest_engine.execute(
        f"SELECT id, name, email, phone, ssn, salary FROM {schema_rand_prefix}.masked_customers ORDER BY id"
    ).fetchall()

    # Check that data was masked correctly
    assert len(res) == 3

    # First row checks
    assert res[0][0] == 1  # id unchanged
    assert res[0][1] == "John Doe"  # name unchanged
    assert len(res[0][2]) == 64  # email should be SHA-256 hash (64 chars)
    assert res[0][3] == "555******567"  # phone partially masked
    assert res[0][4] == "REDACTED"  # SSN redacted
    assert res[0][5] == 50000  # salary rounded to nearest 5000

    # Second row checks
    assert res[1][0] == 2
    assert res[1][1] == "Jane Smith"
    assert len(res[1][2]) == 64  # email hash
    assert res[1][3] == "555******543"
    assert res[1][4] == "REDACTED"
    assert res[1][5] == 70000  # 67800 -> 70000

    # Third row checks
    assert res[2][0] == 3
    assert res[2][1] == "Bob Johnson"
    assert len(res[2][2]) == 64
    assert res[2][3] == "555******234"
    assert res[2][4] == "REDACTED"
    assert res[2][5] == 45000  # 45000 -> 45000 (already rounded)

    source_instance.stop()
    dest_instance.stop()


def test_duckdb_masking_consistency():
    """
    Test that hash masking produces consistent results across multiple runs.
    """
    source_instance = EphemeralDuckDb()
    dest_instance1 = EphemeralDuckDb()
    dest_instance2 = EphemeralDuckDb()

    source_uri = source_instance.start()
    dest_uri1 = dest_instance1.start()
    dest_uri2 = dest_instance2.start()

    schema_rand_prefix = f"test_mask_consistency_{get_random_string(5)}"
    source_engine = sqlalchemy.create_engine(source_uri, poolclass=NullPool)

    # Create test data
    with source_engine.begin() as conn:
        conn.execute(f"DROP SCHEMA IF EXISTS {schema_rand_prefix}")
        conn.execute(f"CREATE SCHEMA {schema_rand_prefix}")
        conn.execute(
            f"""CREATE TABLE {schema_rand_prefix}.users (
                id INTEGER,
                username VARCHAR(100),
                email VARCHAR(100)
            )"""
        )
        conn.execute(
            f"""INSERT INTO {schema_rand_prefix}.users VALUES 
                (1, 'user1', 'user1@example.com'),
                (2, 'user2', 'user2@example.com')
            """
        )

    # Run first ingestion with masking
    result1 = invoke_ingest_command(
        source_uri,
        f"{schema_rand_prefix}.users",
        dest_uri1,
        f"{schema_rand_prefix}.masked_users",
        mask=["email:hash", "username:hash"],
    )
    assert result1.exit_code == 0

    # Run second ingestion with same masking
    result2 = invoke_ingest_command(
        source_uri,
        f"{schema_rand_prefix}.users",
        dest_uri2,
        f"{schema_rand_prefix}.masked_users",
        mask=["email:hash", "username:hash"],
    )
    assert result2.exit_code == 0

    # Get results from both destinations
    dest_engine1 = sqlalchemy.create_engine(dest_uri1, poolclass=NullPool)
    res1 = dest_engine1.execute(
        f"SELECT id, username, email FROM {schema_rand_prefix}.masked_users ORDER BY id"
    ).fetchall()

    dest_engine2 = sqlalchemy.create_engine(dest_uri2, poolclass=NullPool)
    res2 = dest_engine2.execute(
        f"SELECT id, username, email FROM {schema_rand_prefix}.masked_users ORDER BY id"
    ).fetchall()

    # Check that hashes are consistent between runs
    assert res1 == res2

    # Verify hashes are different from original values
    assert res1[0][1] != "user1"
    assert res1[0][2] != "user1@example.com"
    assert len(res1[0][1]) == 64  # SHA-256 hash
    assert len(res1[0][2]) == 64

    source_instance.stop()
    dest_instance1.stop()
    dest_instance2.stop()


def test_duckdb_masking_format_preserving():
    """
    Test format-preserving masking algorithms.
    """
    source_instance = EphemeralDuckDb()
    dest_instance = EphemeralDuckDb()

    source_uri = source_instance.start()
    dest_uri = dest_instance.start()

    schema_rand_prefix = f"test_format_masking_{get_random_string(5)}"
    source_engine = sqlalchemy.create_engine(source_uri, poolclass=NullPool)

    # Create test data
    with source_engine.begin() as conn:
        conn.execute(f"DROP SCHEMA IF EXISTS {schema_rand_prefix}")
        conn.execute(f"CREATE SCHEMA {schema_rand_prefix}")
        conn.execute(
            f"""CREATE TABLE {schema_rand_prefix}.contacts (
                id INTEGER,
                email VARCHAR(100),
                phone VARCHAR(20),
                credit_card VARCHAR(20),
                ssn VARCHAR(15),
                name VARCHAR(100)
            )"""
        )
        conn.execute(
            f"""INSERT INTO {schema_rand_prefix}.contacts VALUES 
                (1, 'alice@example.com', '555-123-4567', '4111-1111-1111-1111', '123-45-6789', 'Alice Brown'),
                (2, 'bob@company.org', '555-987-6543', '5500-0000-0000-0004', '987-65-4321', 'Bob Smith')
            """
        )

    # Run ingestion with format-preserving masks
    result = invoke_ingest_command(
        source_uri,
        f"{schema_rand_prefix}.contacts",
        dest_uri,
        f"{schema_rand_prefix}.masked_contacts",
        mask=[
            "email:email",
            "phone:phone",
            "credit_card:credit_card",
            "ssn:ssn",
            "name:first_letter",
        ],
    )

    assert result.exit_code == 0

    # Verify masked data
    dest_engine = sqlalchemy.create_engine(dest_uri, poolclass=NullPool)
    res = dest_engine.execute(
        f"SELECT id, email, phone, credit_card, ssn, name FROM {schema_rand_prefix}.masked_contacts ORDER BY id"
    ).fetchall()

    # Check format-preserving masks
    assert len(res) == 2

    # Email masking - preserves domain (column 1)
    assert "@example.com" in res[0][1]
    assert "@company.org" in res[1][1]
    assert res[0][1] != "alice@example.com"
    assert res[1][1] != "bob@company.org"

    # Phone masking - shows area code and last digits (column 2)
    assert res[0][2].startswith("555")
    assert "***" in res[0][2]

    # Credit card - shows last 4 digits only (column 3)
    assert res[0][3] == "************1111"
    assert res[1][3] == "************0004"

    # SSN - shows last 4 digits (column 4)
    assert res[0][4] == "***-**-6789"
    assert res[1][4] == "***-**-4321"

    # Name - first letter only (column 5)
    assert res[0][5] == "A**********"  # Alice Brown (11 chars -> 10 stars)
    assert res[1][5] == "B********"  # Bob Smith (9 chars -> 8 stars)

    source_instance.stop()
    dest_instance.stop()


def test_duckdb_masking_numeric_and_date():
    """
    Test numeric masking algorithms.
    """
    source_instance = EphemeralDuckDb()
    dest_instance = EphemeralDuckDb()

    source_uri = source_instance.start()
    dest_uri = dest_instance.start()

    schema_rand_prefix = f"test_numeric_masking_{get_random_string(5)}"
    source_engine = sqlalchemy.create_engine(source_uri, poolclass=NullPool)

    # Create test data
    with source_engine.begin() as conn:
        conn.execute(f"DROP SCHEMA IF EXISTS {schema_rand_prefix}")
        conn.execute(f"CREATE SCHEMA {schema_rand_prefix}")
        conn.execute(
            f"""CREATE TABLE {schema_rand_prefix}.transactions (
                id INTEGER,
                amount DOUBLE,
                age INTEGER,
                score INTEGER,
                notes VARCHAR(100)
            )"""
        )
        conn.execute(
            f"""INSERT INTO {schema_rand_prefix}.transactions VALUES 
                (1, 12345.67, 34, 456, 'Transaction notes 1'),
                (2, 98765.43, 57, 789, 'Transaction notes 2'),
                (3, 5432.10, 28, 234, 'Transaction notes 3')
            """
        )

    # Run ingestion with numeric masks
    result = invoke_ingest_command(
        source_uri,
        f"{schema_rand_prefix}.transactions",
        dest_uri,
        f"{schema_rand_prefix}.masked_transactions",
        mask=["amount:round:1000", "age:round:10", "score:round:100", "notes:redact"],
    )

    assert result.exit_code == 0

    # Verify masked data
    dest_engine = sqlalchemy.create_engine(dest_uri, poolclass=NullPool)
    res = dest_engine.execute(
        f"SELECT id, amount, age, score, notes FROM {schema_rand_prefix}.masked_transactions ORDER BY id"
    ).fetchall()

    # Check numeric masks
    assert len(res) == 3

    # Round masking on amount
    assert res[0][1] == 12000  # 12345.67 -> 12000 (round to 1000)
    assert res[1][1] == 99000  # 98765.43 -> 99000
    assert res[2][1] == 5000  # 5432.10 -> 5000

    # Round masking on age
    assert res[0][2] == 30  # 34 -> 30 (round to 10)
    assert res[1][2] == 60  # 57 -> 60
    assert res[2][2] == 30  # 28 -> 30

    # Round masking on score column
    assert res[0][3] == 500  # 456 -> 500 (round to 100)
    assert res[1][3] == 800  # 789 -> 800
    assert res[2][3] == 200  # 234 -> 200

    # Notes redacted
    assert res[0][4] == "REDACTED"
    assert res[1][4] == "REDACTED"
    assert res[2][4] == "REDACTED"

    source_instance.stop()
    dest_instance.stop()


def test_csv_dest():
    """
    Smoke test to ensure that CSV destination works.
    """
    with (
        tempfile.NamedTemporaryFile("w") as duck_src,
        tempfile.NamedTemporaryFile("w") as csv_dest,
    ):
        duck_src.close()
        csv_dest.close()
        try:
            conn = duckdb.connect(duck_src.name)
            conn.sql(
                """
                CREATE SCHEMA public;
                CREATE TABLE public.testdata(name varchar, age integer);
                INSERT INTO public.testdata(name, age)
                VALUES ('Jhon', 42), ('Lisa', 21), ('Mike', 24), ('Mary', 27);
            """
            )
            conn.close()
            result = invoke_ingest_command(
                f"duckdb:///{duck_src.name}",
                "public.testdata",
                f"csv://{csv_dest.name}",
                "dataset.table",  # unused by csv dest
            )
            assert result.exit_code == 0
            with open(csv_dest.name, "r") as output:
                reader = csv.DictReader(output)
                rows = [row for row in reader]
                assert len(rows) == 4
        finally:
            os.remove(duck_src.name)
            os.remove(csv_dest.name)


@dataclass
class DynamoDBTestConfig:
    db_name: str
    uri: str
    data: List[Dict]


@pytest.fixture(scope="session")
def dynamodb():
    db_name = f"dynamodb_test_{get_random_string(5)}"
    table_cfg = {
        "TableName": db_name,
        "KeySchema": [
            {
                "AttributeName": "id",
                "KeyType": "HASH",
            }
        ],
        "AttributeDefinitions": [
            {"AttributeName": "id", "AttributeType": "S"},
        ],
        "ProvisionedThroughput": {
            "ReadCapacityUnits": 35000,
            "WriteCapacityUnits": 35000,
        },
    }

    items = [
        {"id": {"S": "1"}, "updated_at": {"S": "2024-01-01T00:00:00"}},
        {"id": {"S": "2"}, "updated_at": {"S": "2024-02-01T00:00:00"}},
        {"id": {"S": "3"}, "updated_at": {"S": "2024-03-01T00:00:00"}},
    ]

    def load_test_data(ls):
        client = ls.get_client("dynamodb")
        client.create_table(**table_cfg)
        for item in items:
            client.put_item(TableName=db_name, Item=item)

    def items_to_list(items):
        """converts dynamodb item list to list of dics"""
        result = []
        for i in items:
            entry = {}
            for key, val in i.items():
                entry[key] = list(val.values())[0]
            result.append(entry)
        return result

    local_stack = LocalStackContainer(
        image="localstack/localstack:4.0.3"
    ).with_services("dynamodb")
    local_stack.start()
    wait_for_logs(local_stack, "Ready.")
    load_test_data(local_stack)

    dynamodb_url = urlparse(local_stack.get_url())
    src_uri = (
        f"dynamodb://{dynamodb_url.netloc}?"
        + f"region={local_stack.env['AWS_DEFAULT_REGION']}&"
        + f"access_key_id={local_stack.env['AWS_ACCESS_KEY_ID']}&"
        + f"secret_access_key={local_stack.env['AWS_SECRET_ACCESS_KEY']}"
    )
    yield DynamoDBTestConfig(
        db_name,
        src_uri,
        items_to_list(items),
    )

    local_stack.stop()


def dynamodb_tests() -> Iterable[Callable]:
    def assert_success(result):
        if result.exception is not None:
            traceback.print_exception(*result.exc_info)
            raise AssertionError(result.exception)

    def smoke_test(dest_uri, dest_uri_read, dynamodb):
        dest_table = f"public.dynamodb_{get_random_string(5)}"

        result = invoke_ingest_command(
            dynamodb.uri, dynamodb.db_name, dest_uri, dest_table, "append", "updated_at"
        )
        assert_success(result)

        # CrateDB needs an explicit flush to make data available for reads immediately.
        if dest_uri.startswith("cratedb://"):
            get_query_result(dest_uri_read, f"REFRESH TABLE {dest_table}")

        result = get_query_result(
            dest_uri_read, f"select id, updated_at from {dest_table} ORDER BY id"
        )
        assert len(result) == 3
        for i in range(len(result)):
            assert result[i][0] == dynamodb.data[i]["id"]
            refval = pendulum.parse(dynamodb.data[i]["updated_at"])
            if dest_uri.startswith("cratedb://"):
                assert result[i][1] == refval.int_timestamp * 1000
            else:
                assert result[i][1] == refval

    def append_test(dest_uri, dest_uri_read, dynamodb):
        if dest_uri.startswith("cratedb://"):
            pytest.skip(
                "CrateDB support for 'append' strategy pending, "
                "see https://github.com/crate-workbench/ingestr/issues/6"
            )

        dest_table = f"public.dynamodb_{get_random_string(5)}"

        # we run it twice to assert that the data in destination doesn't change
        for i in range(2):
            result = invoke_ingest_command(
                dynamodb.uri,
                dynamodb.db_name,
                dest_uri,
                dest_table,
                "append",
                "updated_at",
            )

            assert_success(result)

            # CrateDB needs an explicit flush to make data available for reads immediately.
            if dest_uri.startswith("cratedb://"):
                get_query_result(dest_uri_read, f"REFRESH TABLE {dest_table}")

            result = get_query_result(
                dest_uri_read, f"select id, updated_at from {dest_table} ORDER BY id"
            )
            assert len(result) == 3
            for i in range(len(result)):
                assert result[i][0] == dynamodb.data[i]["id"]
                refval = pendulum.parse(dynamodb.data[i]["updated_at"])
                if dest_uri.startswith("cratedb://"):
                    assert result[i][1] == refval.int_timestamp * 1000
                else:
                    assert result[i][1] == refval

    def incremental_test_factory(strategy):
        def incremental_test(dest_uri, dest_uri_read, dynamodb):
            if dest_uri.startswith("cratedb://") and strategy != "replace":
                pytest.skip(
                    "CrateDB support for 'merge' strategy pending, "
                    "see https://github.com/crate/dlt-cratedb/issues/14"
                )
            dest_table = f"public.dynamodb_{get_random_string(5)}"

            result = invoke_ingest_command(
                dynamodb.uri,
                dynamodb.db_name,
                dest_uri,
                dest_table,
                inc_strategy=strategy,
                inc_key="updated_at",
                interval_start="2024-01-01T00:00:00",
                interval_end="2024-02-01T00:01:00",  # upto the second entry
            )
            assert_success(result)

            # CrateDB needs an explicit flush to make data available for reads immediately.
            if dest_uri.startswith("cratedb://"):
                get_query_result(dest_uri_read, f"REFRESH TABLE {dest_table}")

            rows = get_query_result(
                dest_uri_read, f"select id, updated_at from {dest_table} ORDER BY id"
            )
            assert len(rows) == 2
            for i in range(len(rows)):
                assert rows[i][0] == dynamodb.data[i]["id"]
                refval = pendulum.parse(dynamodb.data[i]["updated_at"])
                if dest_uri.startswith("cratedb://"):
                    assert rows[i][1] == refval.int_timestamp * 1000
                else:
                    assert rows[i][1] == refval

            # ingest the rest
            # run it twice to test idempotency
            for _ in range(2):
                result = invoke_ingest_command(
                    dynamodb.uri,
                    dynamodb.db_name,
                    dest_uri,
                    dest_table,
                    inc_strategy=strategy,
                    inc_key="updated_at",
                    interval_start="2024-02-01T00:00:00",  # second entry onwards
                )
                assert_success(result)

                # CrateDB needs an explicit flush to make data available for reads immediately.
                dest_engine = sqlalchemy.create_engine(
                    dest_uri_read, poolclass=NullPool
                )
                if dest_engine.dialect.name == "crate":
                    dest_engine.execute(f"REFRESH TABLE {dest_table}")

                rows = get_query_result(
                    dest_uri_read,
                    f"select id, updated_at from {dest_table} ORDER BY id",
                )
                rows_expected = 3
                if strategy == "replace":
                    # old rows are removed in replace
                    rows_expected = 2

                assert len(rows) == rows_expected
                for row in rows:
                    id = int(row[0]) - 1
                    assert row[0] == dynamodb.data[id]["id"]

                    refval = pendulum.parse(dynamodb.data[id]["updated_at"])
                    if dest_uri.startswith("cratedb://"):
                        assert row[1] == refval.int_timestamp * 1000
                    else:
                        assert row[1] == refval

        # for easier debugging
        incremental_test.__name__ += f"_{strategy}"
        return incremental_test

    strategies = [
        "replace",
        "delete+insert",
        "merge",
    ]
    incremental_tests = [incremental_test_factory(strat) for strat in strategies]

    return [
        smoke_test,
        append_test,
        *incremental_tests,
    ]


@pytest.mark.parametrize(
    "dest", list(DESTINATIONS.values()), ids=list(DESTINATIONS.keys())
)
@pytest.mark.parametrize("testcase", dynamodb_tests())
def test_dynamodb(dest, dynamodb, testcase):
    dest_uri = dest.start()
    dest_uri_read = get_uri_read(dest_uri, dest)
    testcase(dest_uri, dest_uri_read, dynamodb)
    dest.stop()


def get_query_result(uri: str, query: str):
    engine = sqlalchemy.create_engine(uri, poolclass=NullPool)
    with engine.connect() as conn:
        res = conn.execute(query).fetchall()
    engine.dispose()
    return res


def custom_query_tests():
    def replace(
        source_connection_url,
        dest_connection_url,
        dest_connection_url_read: str,
    ):
        if source_connection_url.startswith(
            "mssql://"
        ) and dest_connection_url.startswith("cratedb://"):
            pytest.skip(
                "CrateDB type mapping does not support `DATE` yet, "
                "see https://github.com/crate-workbench/ingestr/issues/4"
            )

        # CrateDB: Compensate for "Type `date` does not support storage".
        updated_at_type = "DATE"
        if dest_connection_url.startswith("cratedb://"):
            updated_at_type = "TIMESTAMP"

        schema = f"testschema_cr_cust_{get_random_string(5)}"
        with sqlalchemy.create_engine(
            source_connection_url, poolclass=NullPool
        ).connect() as conn:
            conn.execute(f"DROP SCHEMA IF EXISTS {schema}")
            conn.execute(f"CREATE SCHEMA {schema}")
            conn.execute(
                f"CREATE TABLE {schema}.orders (id INTEGER, name VARCHAR(255) NOT NULL, updated_at {updated_at_type})"
            )
            conn.execute(
                f"CREATE TABLE {schema}.order_items (id INTEGER, order_id INTEGER NOT NULL, subname VARCHAR(255) NOT NULL)"
            )
            conn.execute(
                f"INSERT INTO {schema}.orders (id, name, updated_at) VALUES (1, 'First Order', '2024-01-01'), (2, 'Second Order', '2024-01-01'), (3, 'Third Order', '2024-01-01'), (4, 'Fourth Order', '2024-01-01')"
            )
            conn.execute(
                f"INSERT INTO {schema}.order_items (id, order_id, subname) VALUES (1, 1, 'Item 1 for First Order'), (2, 1, 'Item 2 for First Order'), (3, 2, 'Item 1 for Second Order'), (4, 3, 'Item 1 for Third Order')"
            )
            res = conn.execute(f"select count(*) from {schema}.orders").fetchall()
            assert res[0][0] == 4
            res = conn.execute(f"select count(*) from {schema}.order_items").fetchall()
            assert res[0][0] == 4

        if dest_connection_url.startswith("clickhouse"):
            get_query_result(
                dest_connection_url, f"CREATE DATABASE IF NOT EXISTS {schema}"
            )

        result = invoke_ingest_command(
            source_connection_url,
            f"query:select oi.*, o.updated_at from {schema}.order_items oi join {schema}.orders o on oi.order_id = o.id",
            dest_connection_url,
            f"{schema}.output",
            run_in_subprocess=True,
        )

        assert result.exit_code == 0

        # CrateDB needs an explicit flush to make data available for reads immediately.
        if dest_connection_url.startswith("cratedb://"):
            get_query_result(dest_connection_url_read, f"REFRESH TABLE {schema}.output")

        res = get_query_result(
            dest_connection_url_read,
            f"select id, order_id, subname, updated_at from {schema}.output order by id asc",
        )

        assert len(res) == 4
        if dest_connection_url.startswith("cratedb://"):
            assert (1, 1, "Item 1 for First Order", 1704067200000) in res
            assert (2, 1, "Item 2 for First Order", 1704067200000) in res
            assert (3, 2, "Item 1 for Second Order", 1704067200000) in res
            assert (4, 3, "Item 1 for Third Order", 1704067200000) in res
        else:
            assert res[0] == (1, 1, "Item 1 for First Order", as_datetime("2024-01-01"))
            assert res[1] == (2, 1, "Item 2 for First Order", as_datetime("2024-01-01"))
            assert res[2] == (
                3,
                2,
                "Item 1 for Second Order",
                as_datetime("2024-01-01"),
            )
            assert res[3] == (4, 3, "Item 1 for Third Order", as_datetime("2024-01-01"))

    def merge(
        source_connection_url,
        dest_connection_url,
        dest_connection_url_read: str,
    ):
        if dest_connection_url.startswith("cratedb://"):
            pytest.skip(
                "CrateDB support for 'merge' strategy pending, "
                "see https://github.com/crate/dlt-cratedb/issues/14"
            )

        # CrateDB: Compensate for "Type `date` does not support storage".
        updated_at_type = "DATE"
        if dest_connection_url.startswith("cratedb://"):
            updated_at_type = "TIMESTAMP"
        schema = f"testschema_merge_cust_{get_random_string(5)}"
        source_engine = sqlalchemy.create_engine(
            source_connection_url, poolclass=NullPool
        )
        with source_engine.begin() as conn:
            conn.execute(f"DROP SCHEMA IF EXISTS {schema}")
            conn.execute(f"CREATE SCHEMA {schema}")
            conn.execute(
                f"CREATE TABLE {schema}.orders (id INTEGER, name VARCHAR(255) NOT NULL, updated_at {updated_at_type})"
            )
            conn.execute(
                f"CREATE TABLE {schema}.order_items (id INTEGER, order_id INTEGER NOT NULL, subname VARCHAR(255) NOT NULL)"
            )
            conn.execute(
                f"INSERT INTO {schema}.orders (id, name, updated_at) VALUES (1, 'First Order', '2024-01-01'), (2, 'Second Order', '2024-01-01'), (3, 'Third Order', '2024-01-01'), (4, 'Fourth Order', '2024-01-01')"
            )
            conn.execute(
                f"INSERT INTO {schema}.order_items (id, order_id, subname) VALUES (1, 1, 'Item 1 for First Order'), (2, 1, 'Item 2 for First Order'), (3, 2, 'Item 1 for Second Order'), (4, 3, 'Item 1 for Third Order')"
            )

        if dest_connection_url.startswith("clickhouse"):
            get_query_result(
                dest_connection_url, f"CREATE DATABASE IF NOT EXISTS {schema}"
            )

        def run():
            result = invoke_ingest_command(
                source_connection_url,
                f"query:select oi.*, o.updated_at from {schema}.order_items oi join {schema}.orders o on oi.order_id = o.id where o.updated_at > :interval_start",
                dest_connection_url,
                f"{schema}.output",
                inc_strategy="merge",
                inc_key="updated_at",
                primary_key="id",
                run_in_subprocess=True,
            )
            assert result.exit_code == 0

        # Initial run to get all data
        run()

        # CrateDB needs an explicit flush to make data available for reads immediately.
        if dest_connection_url.startswith("cratedb://"):
            get_query_result(dest_connection_url_read, f"REFRESH TABLE {schema}.output")

        res = get_query_result(
            dest_connection_url_read,
            f"select id, order_id, subname, updated_at, _dlt_load_id from {schema}.output order by id asc",
        )

        assert len(res) == 4
        initial_load_id = res[0][4]
        assert all(r[4] == initial_load_id for r in res)
        assert res[0] == (
            1,
            1,
            "Item 1 for First Order",
            as_datetime("2024-01-01"),
            initial_load_id,
        )
        assert res[1] == (
            2,
            1,
            "Item 2 for First Order",
            as_datetime("2024-01-01"),
            initial_load_id,
        )
        assert res[2] == (
            3,
            2,
            "Item 1 for Second Order",
            as_datetime("2024-01-01"),
            initial_load_id,
        )
        assert res[3] == (
            4,
            3,
            "Item 1 for Third Order",
            as_datetime("2024-01-01"),
            initial_load_id,
        )

        # Run again - should get same load_id since no changes
        run()

        # CrateDB needs an explicit flush to make data available for reads immediately.
        if dest_connection_url.startswith("cratedb://"):
            get_query_result(dest_connection_url_read, f"REFRESH TABLE {schema}.output")

        res = get_query_result(
            dest_connection_url_read,
            f"select id, order_id, subname, updated_at, _dlt_load_id from {schema}.output order by id asc",
        )
        assert len(res) == 4
        assert all(r[4] == initial_load_id for r in res)

        # Update an order item and its order's updated_at
        with source_engine.begin() as conn:
            conn.execute(
                f"UPDATE {schema}.order_items SET subname = 'Item 1 for Second Order - new' WHERE id = 3"
            )
            conn.execute(
                f"UPDATE {schema}.orders SET updated_at = '2024-01-02' WHERE id = 2"
            )

        # Run again - should see updated data with new load_id
        run()

        # CrateDB needs an explicit flush to make data available for reads immediately.
        if dest_connection_url.startswith("cratedb://"):
            get_query_result(dest_connection_url_read, f"REFRESH TABLE {schema}.output")

        res = get_query_result(
            dest_connection_url_read,
            f"select id, order_id, subname, updated_at, _dlt_load_id from {schema}.output order by id asc",
        )

        assert len(res) == 4
        assert res[0] == (
            1,
            1,
            "Item 1 for First Order",
            as_datetime("2024-01-01"),
            res[0][4],
        )
        assert res[1] == (
            2,
            1,
            "Item 2 for First Order",
            as_datetime("2024-01-01"),
            res[1][4],
        )
        assert res[2] == (
            3,
            2,
            "Item 1 for Second Order - new",
            as_datetime("2024-01-02"),
            res[2][4],
        )
        assert res[3] == (
            4,
            3,
            "Item 1 for Third Order",
            as_datetime("2024-01-01"),
            res[3][4],
        )

    return [
        replace,
        merge,
    ]


@pytest.mark.parametrize(
    "dest", list(DESTINATIONS.values()), ids=list(DESTINATIONS.keys())
)
@pytest.mark.parametrize("source", list(SOURCES.values()), ids=list(SOURCES.keys()))
@pytest.mark.parametrize("testcase", custom_query_tests())
def test_custom_query(testcase, source, dest):
    with ThreadPoolExecutor() as executor:
        source_future = executor.submit(source.start)
        dest_future = executor.submit(dest.start)
        source_uri = source_future.result()
        dest_uri = dest_future.result()
    dest_uri_read = get_uri_read(dest_uri, dest)
    testcase(source_uri, dest_uri, dest_uri_read)
    source.stop()
    dest.stop()


# Integration testing when the access token is not provided, and it is only for the resource "repo_events
@pytest.mark.parametrize(
    "dest", list(DESTINATIONS.values()), ids=list(DESTINATIONS.keys())
)
def test_github_to_duckdb(dest):
    dest_uri = dest.start()
    dest_uri_read = get_uri_read(dest_uri, dest)
    source_uri = "github://?owner=bruin-data&repo=ingestr"
    source_table = "repo_events"

    dest_table = "dest.github_repo_events"
    res = invoke_ingest_command(source_uri, source_table, dest_uri, dest_table)

    assert res.exit_code == 0

    dest_engine = sqlalchemy.create_engine(dest_uri_read, poolclass=NullPool)
    # CrateDB needs an explicit flush to make data available for reads immediately.
    if dest_engine.dialect.name == "crate":
        dest_engine.execute(f"REFRESH TABLE {dest_table}")
    res = dest_engine.execute(f"select count(*) from {dest_table}").fetchall()
    dest_engine.dispose()
    assert len(res) > 0


def appstore_test_cases() -> Iterable[Callable]:
    app_download_testdata = (
        "Date\tApp Apple Identifier\tCounts\tProcessing Date\tApp Name\tDownload Type\tApp Version\tDevice\tPlatform Version\tSource Type\tSource Info\tCampaign\tPage Type\tPage Title\tPre-Order\tTerritory\n"
        "2025-01-01\t1\t590\t2025-01-01\tAcme Inc\tAuto-update\t4.2.40\tiPhone\tiOS 18.1\tApp Store search\t\t\tNo page\tNo page\t\tFR\n"
        "2025-01-01\t1\t16\t2025-01-01\tAcme Inc\tAuto-update\t4.2.40\tiPhone\tiOS 18.1\tApp referrer\tcom.burbn.instagram\t\tStore sheet\tDefault custom product page\t\tSG\n"
        "2025-01-01\t1\t11\t2025-01-01\tAcme Inc\tAuto-update\t4.2.40\tiPhone\tiOS 18.3\tApp Store search\t\t\tNo page\tNo page\t\tMX\n"
    )

    app_download_testdata_extended = (
        "Date\tApp Apple Identifier\tCounts\tProcessing Date\tApp Name\tDownload Type\tApp Version\tDevice\tPlatform Version\tSource Type\tSource Info\tCampaign\tPage Type\tPage Title\tPre-Order\tTerritory\n"
        "2025-01-02\t1\t590\t2025-01-02\tAcme Inc\tAuto-update\t4.2.40\tiPhone\tiOS 18.1\tApp Store search\t\t\tNo page\tNo page\t\tFR\n"
        "2025-01-02\t1\t16\t2025-01-02\tAcme Inc\tAuto-update\t4.2.40\tiPhone\tiOS 18.1\tApp referrer\tcom.burbn.instagram\t\tStore sheet\tDefault custom product page\t\tSG\n"
        "2025-01-02\t1\t11\t2025-01-02\tAcme Inc\tAuto-update\t4.2.40\tiPhone\tiOS 18.3\tApp Store search\t\t\tNo page\tNo page\t\tMX\n"
    )

    api_key = base64.b64encode(b"MOCK_KEY").decode()

    def create_mock_response(data: str) -> requests.Response:
        res = requests.Response()
        buffer = io.BytesIO()
        archive = gzip.GzipFile(fileobj=buffer, mode="w")
        archive.write(data.encode())
        archive.close()
        buffer.seek(0)
        res.status_code = 200
        res.raw = buffer
        return res

    def test_no_report_instances_found(dest_uri, dest_uri_read):
        """
        When there are no report instances for the given date range,
        NoReportsError should be raised.
        """
        client = MagicMock()
        client.list_analytics_report_requests = MagicMock(
            return_value=AnalyticsReportRequestsResponse(
                [
                    ReportRequest(
                        type="analyticsReportRequests",
                        id="123",
                        attributes=ReportRequestAttributes(
                            accessType="ONGOING", stoppedDueToInactivity=False
                        ),
                    )
                ],
                None,
                None,
            )
        )
        client.list_analytics_reports = MagicMock(
            return_value=AnalyticsReportResponse(
                [
                    Report(
                        type="analyticsReports",
                        id="123",
                        attributes=ReportAttributes(
                            name="app-downloads-detailed", category="USER"
                        ),
                    )
                ],
                None,
                None,
            )
        )
        client.list_report_instances = MagicMock(
            return_value=AnalyticsReportInstancesResponse(
                [
                    ReportInstance(
                        type="analyticsReportInstances",
                        id="123",
                        attributes=ReportInstanceAttributes(
                            granularity="DAILY", processingDate="2024-01-03"
                        ),
                    )
                ],
                None,
                None,
            )
        )

        with patch("ingestr.src.appstore.client.AppStoreConnectClient") as mock_client:
            mock_client.return_value = client
            schema_rand_prefix = f"testschema_appstore_{get_random_string(5)}"
            dest_table = f"{schema_rand_prefix}.app_downloads_{get_random_string(5)}"
            result = invoke_ingest_command(
                f"appstore://?key_id=123&issuer_id=123&key_base64={api_key}&app_id=123",
                "app-downloads-detailed",
                dest_uri,
                dest_table,
                interval_start="2024-01-01",
                interval_end="2024-01-02",
                print_output=False,
            )
            assert has_exception(result.exception, NoReportsFoundError)

    def test_no_ongoing_reports_found(dest_uri, dest_uri_read):
        """
        when there are no ongoing reports, or ongoing reports that have
        been stopped due to inactivity, NoOngoingReportRequestsFoundError should be raised.
        """
        client = MagicMock()
        client.list_analytics_report_requests = MagicMock(
            return_value=AnalyticsReportRequestsResponse(
                [
                    ReportRequest(
                        type="analyticsReportRequests",
                        id="123",
                        attributes=ReportRequestAttributes(
                            accessType="ONE_TIME_SNAPSHOT", stoppedDueToInactivity=False
                        ),
                    ),
                    ReportRequest(
                        type="analyticsReportRequests",
                        id="124",
                        attributes=ReportRequestAttributes(
                            accessType="ONGOING", stoppedDueToInactivity=True
                        ),
                    ),
                ],
                None,
                None,
            )
        )
        with patch("ingestr.src.appstore.client.AppStoreConnectClient") as mock_client:
            mock_client.return_value = client
            schema_rand_prefix = f"testschema_appstore_{get_random_string(5)}"
            dest_table = f"{schema_rand_prefix}.app_downloads_{get_random_string(5)}"
            result = invoke_ingest_command(
                f"appstore://?key_id=123&issuer_id=123&key_base64={api_key}&app_id=123",
                "app-downloads-detailed",
                dest_uri,
                dest_table,
                interval_start="2024-01-01",
                interval_end="2024-01-02",
                print_output=False,
            )
            assert has_exception(result.exception, NoOngoingReportRequestsFoundError)

    def test_no_such_report(dest_uri, dest_uri_read):
        """
        when there is no report with the given name, NoSuchReportError should be raised.
        """
        client = MagicMock()
        client.list_analytics_report_requests = MagicMock(
            return_value=AnalyticsReportRequestsResponse(
                [
                    ReportRequest(
                        type="analyticsReportRequests",
                        id="123",
                        attributes=ReportRequestAttributes(
                            accessType="ONGOING", stoppedDueToInactivity=False
                        ),
                    )
                ],
                None,
                None,
            )
        )
        client.list_analytics_reports = MagicMock(
            return_value=AnalyticsReportResponse(
                [],
                None,
                None,
            )
        )

        with patch("ingestr.src.appstore.client.AppStoreConnectClient") as mock_client:
            mock_client.return_value = client
            schema_rand_prefix = f"testschema_appstore_{get_random_string(5)}"
            dest_table = f"{schema_rand_prefix}.app_downloads_{get_random_string(5)}"
            result = invoke_ingest_command(
                f"appstore://?key_id=123&issuer_id=123&key_base64={api_key}&app_id=123",
                "app-downloads-detailed",
                dest_uri,
                dest_table,
                interval_start="2024-01-01",
                interval_end="2024-01-02",
                print_output=False,
            )
            assert has_exception(result.exception, NoSuchReportError)

    def test_successful_ingestion(dest_uri, dest_uri_read):
        """
        When there are report instances for the given date range, the data should be ingested
        """

        if dest_uri.startswith("cratedb://"):
            pytest.skip(
                "CrateDB type mapping does not support `DATE` yet, "
                "see https://github.com/crate-workbench/ingestr/issues/4"
            )

        client = MagicMock()
        client.list_analytics_report_requests = MagicMock(
            return_value=AnalyticsReportRequestsResponse(
                [
                    ReportRequest(
                        type="analyticsReportRequests",
                        id="123",
                        attributes=ReportRequestAttributes(
                            accessType="ONGOING", stoppedDueToInactivity=False
                        ),
                    )
                ],
                None,
                None,
            )
        )
        client.list_analytics_reports = MagicMock(
            return_value=AnalyticsReportResponse(
                [
                    Report(
                        type="analyticsReports",
                        id="123",
                        attributes=ReportAttributes(
                            name="app-downloads-detailed", category="USER"
                        ),
                    )
                ],
                None,
                None,
            )
        )

        client.list_report_instances = MagicMock(
            return_value=AnalyticsReportInstancesResponse(
                [
                    ReportInstance(
                        type="analyticsReportInstances",
                        id="123",
                        attributes=ReportInstanceAttributes(
                            granularity="DAILY", processingDate="2025-01-01"
                        ),
                    )
                ],
                None,
                None,
            )
        )

        client.list_report_segments = MagicMock(
            return_value=AnalyticsReportSegmentsResponse(
                [
                    ReportSegment(
                        type="analyticsReportSegments",
                        id="123",
                        attributes=ReportSegmentAttributes(
                            checksum="checksum-0",
                            url="http://example.com/report.csv",  # we'll monkey patch requests.get to return this file
                            sizeInBytes=123,
                        ),
                    )
                ],
                None,
                None,
            )
        )

        with patch("ingestr.src.appstore.client.AppStoreConnectClient") as mock_client:
            mock_client.return_value = client
            with patch("requests.get") as mock_get:
                mock_get.return_value = create_mock_response(app_download_testdata)
                schema_rand_prefix = f"testschema_appstore_{get_random_string(5)}"
                dest_table = (
                    f"{schema_rand_prefix}.app_downloads_{get_random_string(5)}"
                )
                result = invoke_ingest_command(
                    f"appstore://?key_id=123&issuer_id=123&key_base64={api_key}",
                    "app-downloads-detailed:123",  # moved the app ID to the table name to ensure that also works
                    dest_uri,
                    dest_table,
                    interval_start="2025-01-01",
                    interval_end="2025-01-02",
                )

        assert result.exit_code == 0

        dest_engine = sqlalchemy.create_engine(dest_uri_read)
        # CrateDB needs an explicit flush to make data available for reads immediately.
        if dest_engine.dialect.name == "crate":
            dest_engine.execute(f"REFRESH TABLE {dest_table}")
        count = dest_engine.execute(f"select count(*) from {dest_table}").fetchone()[0]
        dest_engine.dispose()
        assert count == 3

    def test_incremental_ingestion(dest_uri, dest_uri_read):
        """
        when the pipeline is run till a specific end date, the next ingestion
        should load data from the last processing date, given that last_date is not provided
        """

        if dest_uri.startswith("cratedb://"):
            pytest.skip(
                "CrateDB type mapping does not support `DATE` yet, "
                "see https://github.com/crate-workbench/ingestr/issues/4"
            )

        client = MagicMock()
        client.list_analytics_report_requests = MagicMock(
            return_value=AnalyticsReportRequestsResponse(
                [
                    ReportRequest(
                        type="analyticsReportRequests",
                        id="123",
                        attributes=ReportRequestAttributes(
                            accessType="ONGOING", stoppedDueToInactivity=False
                        ),
                    )
                ],
                None,
                None,
            )
        )
        client.list_analytics_reports = MagicMock(
            return_value=AnalyticsReportResponse(
                [
                    Report(
                        type="analyticsReports",
                        id="123",
                        attributes=ReportAttributes(
                            name="app-downloads-detailed", category="USER"
                        ),
                    )
                ],
                None,
                None,
            )
        )

        client.list_report_instances = MagicMock(
            return_value=AnalyticsReportInstancesResponse(
                [
                    ReportInstance(
                        type="analyticsReportInstances",
                        id="123",
                        attributes=ReportInstanceAttributes(
                            granularity="DAILY", processingDate="2025-01-01"
                        ),
                    ),
                    ReportInstance(
                        type="analyticsReportInstances",
                        id="123",
                        attributes=ReportInstanceAttributes(
                            granularity="DAILY", processingDate="2025-01-02"
                        ),
                    ),
                ],
                None,
                None,
            )
        )

        client.list_report_segments = MagicMock(
            return_value=AnalyticsReportSegmentsResponse(
                [
                    ReportSegment(
                        type="analyticsReportSegments",
                        id="123",
                        attributes=ReportSegmentAttributes(
                            checksum="checksum-0",
                            url="http://example.com/report.csv",  # we'll monkey patch requests.get to return this file
                            sizeInBytes=123,
                        ),
                    )
                ],
                None,
                None,
            )
        )

        with patch("ingestr.src.appstore.client.AppStoreConnectClient") as mock_client:
            mock_client.return_value = client
            with patch("requests.get") as mock_get:
                mock_get.return_value = create_mock_response(app_download_testdata)
                schema_rand_prefix = f"testschema_appstore_{get_random_string(5)}"
                dest_table = (
                    f"{schema_rand_prefix}.app_downloads_{get_random_string(5)}"
                )
                result = invoke_ingest_command(
                    f"appstore://?key_id=123&issuer_id=123&key_base64={api_key}&app_id=123",
                    "app-downloads-detailed",
                    dest_uri,
                    dest_table,
                    interval_end="2025-01-01",
                )

        assert result.exit_code == 0

        dest_engine = sqlalchemy.create_engine(dest_uri_read)
        # CrateDB needs an explicit flush to make data available for reads immediately.
        if dest_engine.dialect.name == "crate":
            dest_engine.execute(f"REFRESH TABLE {dest_table}")
        count = dest_engine.execute(f"select count(*) from {dest_table}").fetchone()[0]
        dest_engine.dispose()
        assert count == 3

        # now run the pipeline again without an end date
        with patch("ingestr.src.appstore.client.AppStoreConnectClient") as mock_client:
            mock_client.return_value = client
            with patch("requests.get") as mock_get:
                mock_get.side_effect = [
                    create_mock_response(app_download_testdata),
                    create_mock_response(app_download_testdata_extended),
                ]
                schema_rand_prefix = f"testschema_appstore_{get_random_string(5)}"
                dest_table = (
                    f"{schema_rand_prefix}.app_downloads_{get_random_string(5)}"
                )
                result = invoke_ingest_command(
                    f"appstore://?key_id=123&issuer_id=123&key_base64={api_key}&app_id=123",
                    "app-downloads-detailed",
                    dest_uri,
                    dest_table,
                )

        assert result.exit_code == 0

        dest_engine = sqlalchemy.create_engine(dest_uri_read)
        # CrateDB needs an explicit flush to make data available for reads immediately.
        if dest_engine.dialect.name == "crate":
            dest_engine.execute(f"REFRESH TABLE {dest_table}")
        count = dest_engine.execute(f"select count(*) from {dest_table}").fetchone()[0]
        assert count == 6
        assert (
            len(
                dest_engine.execute(
                    f"select processing_date from {dest_table} group by 1"
                ).fetchall()
            )
            == 2
        )
        dest_engine.dispose()

    return [
        test_no_report_instances_found,
        test_no_ongoing_reports_found,
        test_no_such_report,
        test_successful_ingestion,
        test_incremental_ingestion,
    ]


@pytest.mark.parametrize(
    "dest", list(DESTINATIONS.values()), ids=list(DESTINATIONS.keys())
)
@pytest.mark.parametrize("test_case", appstore_test_cases())
def test_appstore(dest, test_case):
    dest_uri = dest.start()
    dest_uri_read = get_uri_read(dest_uri, dest)
    test_case(dest_uri, dest_uri_read)
    dest.stop()


def fs_test_cases(
    protocol: str,
    target_fs: str,
    auth: str,
) -> Iterable[Callable]:
    """
    Tests for filesystem based sources
    """
    testdata = (
        "name,phone,email,country\n"
        "Rajah Roach,1-459-646-7421,adipiscing.ligula@outlook.net,Austria\n"
        "Kiayada Jackson,(341) 484-6523,velit.egestas.lacinia@hotmail.couk,Norway\n"
        "Bradley Grant,1-329-268-4178,leo.cras@hotmail.org,Chile\n"
        "Damian Velasquez,(462) 744-9637,phasellus.fermentum@outlook.ca,South Africa\n"
        "Rina Nicholson,(201) 971-6463,neque.nullam.ut@yahoo.net,Brazil\n"
    )
    testdata_extended = (
        "name,phone,email,country\n"
        "Irene Douglas,(223) 971-6463,flying.fish.kick@gmail.com,UK\n"
    )
    test_fs = MemoryFileSystem()

    # for CSV tests
    with test_fs.open("/data.csv", "w") as f:
        f.write(testdata)
    with test_fs.open("/data.csv.gz", "wb") as f:
        with gzip.GzipFile(fileobj=f, mode="wb") as gz:
            gz.write(testdata.encode())

    # for Glob tests
    with test_fs.open("/data2.csv", "w") as f:
        f.write(testdata_extended)

    # For Parquet tests
    with test_fs.open("/data.parquet", "wb") as f:
        table = pa.csv.read_csv(io.BytesIO(testdata.encode()))
        pya_parquet.write_table(table, f)
    with io.BytesIO() as buf:
        pya_parquet.write_table(table, buf)
        buf.seek(0)
        with test_fs.open("/data.parquet.gz", "wb") as f:
            with gzip.GzipFile(fileobj=f, mode="wb") as gz:
                gz.write(buf.getvalue())

    # For JSONL tests
    with test_fs.open("/data.jsonl", "w") as f:
        reader = csv.DictReader(io.StringIO(testdata))
        for row in reader:
            json.dump(row, f)
            f.write("\n")
    with test_fs.open("/data.jsonl.gz", "wb") as f:
        with gzip.GzipFile(fileobj=f, mode="wb") as gz:
            reader = csv.DictReader(io.StringIO(testdata))
            for row in reader:
                gz.write(json.dumps(row).encode())
                gz.write(b"\n")

    # for testing unsupported files
    with test_fs.open("/bin/data.bin", "w") as f:
        f.write("BINARY")

    def glob_files_override(fs_client, _, file_glob):
        return glob_files(fs_client, "memory://", file_glob)

    def assert_rows(dest_uri, dest_table, n):
        engine = sqlalchemy.create_engine(dest_uri)
        with engine.connect() as conn:
            # CrateDB needs an explicit flush to make data available for reads immediately.
            if engine.dialect.name == "crate":
                conn.execute(f"REFRESH TABLE {dest_table}")
            rows = conn.execute(f"select count(*) from {dest_table}").fetchall()
            assert len(rows) == 1
            assert rows[0] == (n,)
        engine.dispose()

    def test_empty_source_uri(dest_uri, dest_uri_read):
        """
        When the source URI is empty, an error should be raised.
        """
        schema = f"testschema_fs_{get_random_string(5)}"
        result = invoke_ingest_command(
            f"{protocol}://bucket?{auth}",
            "",
            dest_uri,
            f"{schema}.test",
            print_output=False,
        )
        assert has_exception(result.exception, InvalidBlobTableError)

    def test_unsupported_file_format(dest_uri, dest_uri_read):
        """
        When the source file is not one of [csv, parquet, jsonl] it should
        raise an exception
        """
        with (
            patch(target_fs),
            patch("ingestr.src.filesystem.glob_files", wraps=glob_files_override),
        ):
            schema_rand_prefix = f"testschema_fs_{get_random_string(5)}"
            dest_table = f"{schema_rand_prefix}.fs_{get_random_string(5)}"
            result = invoke_ingest_command(
                f"{protocol}://bucket?{auth}",
                "/bin/data.bin",
                dest_uri,
                dest_table,
                print_output=False,
            )
            assert result.exit_code != 0
            assert has_exception(result.exception, ValueError)

    def test_missing_credentials(dest_uri, dest_uri_read):
        """
        When the credentials are missing, an error should be raised.
        """
        schema_rand_prefix = f"testschema_fs_{get_random_string(5)}"
        dest_table = f"{schema_rand_prefix}.fs_{get_random_string(5)}"
        result = invoke_ingest_command(
            f"{protocol}://bucket",
            "/data.csv",
            dest_uri,
            dest_table,
            print_output=False,
        )
        assert result.exit_code != 0

    def test_csv_load(dest_uri, dest_uri_read):
        """
        When the source URI is a CSV file, the data should be ingested.
        """
        with (
            patch(target_fs) as target_fs_mock,
            patch("ingestr.src.filesystem.glob_files", wraps=glob_files_override),
        ):
            target_fs_mock.return_value = test_fs
            schema_rand_prefix = f"testschema_fs_{get_random_string(5)}"
            dest_table = f"{schema_rand_prefix}.fs_{get_random_string(5)}"
            result = invoke_ingest_command(
                f"{protocol}://bucket?{auth}",
                "/data.csv",
                dest_uri,
                dest_table,
            )
            assert result.exit_code == 0
            assert_rows(dest_uri_read, dest_table, 5)

    def test_csv_gz_load(dest_uri, dest_uri_read):
        """When the source URI is a gzipped CSV file, the data should be ingested."""
        with (
            patch(target_fs) as target_fs_mock,
            patch("ingestr.src.filesystem.glob_files", wraps=glob_files_override),
        ):
            target_fs_mock.return_value = test_fs
            schema_rand_prefix = f"testschema_fs_{get_random_string(5)}"
            dest_table = f"{schema_rand_prefix}.fs_{get_random_string(5)}"
            result = invoke_ingest_command(
                f"{protocol}://bucket?{auth}",
                "/data.csv.gz",
                dest_uri,
                dest_table,
            )
            assert result.exit_code == 0
            assert_rows(dest_uri_read, dest_table, 5)

    def test_parquet_load(dest_uri, dest_uri_read):
        """
        When the source URI is a Parquet file, the data should be ingested.
        """
        with (
            patch(target_fs) as target_fs_mock,
            patch("ingestr.src.filesystem.glob_files", wraps=glob_files_override),
        ):
            target_fs_mock.return_value = test_fs
            schema_rand_prefix = f"testschema_fs_{get_random_string(5)}"
            dest_table = f"{schema_rand_prefix}.fs_{get_random_string(5)}"
            result = invoke_ingest_command(
                f"{protocol}://bucket?{auth}",
                "/data.parquet",
                dest_uri,
                dest_table,
            )
            assert result.exit_code == 0
            assert_rows(dest_uri_read, dest_table, 5)

    def test_parquet_gz_load(dest_uri, dest_uri_read):
        """When the source URI is a gzipped Parquet file, the data should be ingested."""
        with (
            patch(target_fs) as target_fs_mock,
            patch("ingestr.src.filesystem.glob_files", wraps=glob_files_override),
        ):
            target_fs_mock.return_value = test_fs
            schema_rand_prefix = f"testschema_fs_{get_random_string(5)}"
            dest_table = f"{schema_rand_prefix}.fs_{get_random_string(5)}"
            result = invoke_ingest_command(
                f"{protocol}://bucket?{auth}",
                "/data.parquet.gz",
                dest_uri,
                dest_table,
            )
            assert result.exit_code == 0
            assert_rows(dest_uri_read, dest_table, 5)

    def test_jsonl_load(dest_uri, dest_uri_read):
        """
        When the source URI is a JSONL file, the data should be ingested.
        """
        with (
            patch(target_fs) as target_fs_mock,
            patch("ingestr.src.filesystem.glob_files", wraps=glob_files_override),
        ):
            target_fs_mock.return_value = test_fs
            schema_rand_prefix = f"testschema_fs_{get_random_string(5)}"
            dest_table = f"{schema_rand_prefix}.fs_{get_random_string(5)}"
            result = invoke_ingest_command(
                f"{protocol}://bucket?{auth}",
                "/data.jsonl",
                dest_uri,
                dest_table,
            )
            assert result.exit_code == 0
            assert_rows(dest_uri_read, dest_table, 5)

    def test_jsonl_gz_load(dest_uri, dest_uri_read):
        """When the source URI is a gzipped JSONL file, the data should be ingested."""
        with (
            patch(target_fs) as target_fs_mock,
            patch("ingestr.src.filesystem.glob_files", wraps=glob_files_override),
        ):
            target_fs_mock.return_value = test_fs
            schema_rand_prefix = f"testschema_fs_{get_random_string(5)}"
            dest_table = f"{schema_rand_prefix}.fs_{get_random_string(5)}"
            result = invoke_ingest_command(
                f"{protocol}://bucket?{auth}",
                "/data.jsonl.gz",
                dest_uri,
                dest_table,
            )
            assert result.exit_code == 0
            assert_rows(dest_uri_read, dest_table, 5)

    def test_glob_load(dest_uri, dest_uri_read):
        """
        When the source URI is a glob pattern, all files matching the pattern should be ingested
        """
        with (
            patch(target_fs) as target_fs_mock,
            patch("ingestr.src.filesystem.glob_files", wraps=glob_files_override),
        ):
            target_fs_mock.return_value = test_fs
            schema_rand_prefix = f"testschema_fs_{get_random_string(5)}"
            dest_table = f"{schema_rand_prefix}.fs_{get_random_string(5)}"
            result = invoke_ingest_command(
                f"{protocol}://bucket?{auth}",
                "/*.csv",
                dest_uri,
                dest_table,
            )
            assert result.exit_code == 0
            assert_rows(dest_uri_read, dest_table, 6)

    def test_compound_table_name(dest_uri, dest_uri_read):
        """
        When table contains both the bucket name and the file glob,
        loads should be successful.
        """
        with (
            patch(target_fs) as target_fs_mock,
            patch("ingestr.src.filesystem.glob_files", wraps=glob_files_override),
        ):
            target_fs_mock.return_value = test_fs
            schema_rand_prefix = f"testschema_fs_{get_random_string(5)}"
            dest_table = f"{schema_rand_prefix}.fs_{get_random_string(5)}"
            result = invoke_ingest_command(
                f"{protocol}://?{auth}",
                "bucket/*.csv",
                dest_uri,
                dest_table,
            )
            assert result.exit_code == 0
            assert_rows(dest_uri_read, dest_table, 6)

    def test_uri_precedence(dest_uri, dest_uri_read):
        """
        When file glob is present in both URI and Source Table,
        the URI glob should be used
        """

        with (
            patch(target_fs) as target_fs_mock,
            patch("ingestr.src.filesystem.glob_files", wraps=glob_files_override),
        ):
            target_fs_mock.return_value = test_fs
            schema_rand_prefix = f"testschema_fs_{get_random_string(5)}"
            dest_table = f"{schema_rand_prefix}.fs_{get_random_string(5)}"
            result = invoke_ingest_command(
                f"{protocol}://bucket/*.csv?{auth}",
                "/path/to/file",  # if this is used, it should result in an error
                dest_uri,
                dest_table,
            )
            assert result.exit_code == 0
            assert_rows(dest_uri_read, dest_table, 6)

    return [
        test_empty_source_uri,
        test_missing_credentials,
        test_unsupported_file_format,
        test_csv_load,
        test_csv_gz_load,
        test_parquet_load,
        test_parquet_gz_load,
        test_jsonl_load,
        test_jsonl_gz_load,
        test_glob_load,
        test_compound_table_name,
        test_uri_precedence,
    ]


@pytest.mark.parametrize(
    "dest", list(DESTINATIONS.values()), ids=list(DESTINATIONS.keys())
)
@pytest.mark.parametrize(
    "test_case",
    fs_test_cases(
        "gs",
        "gcsfs.GCSFileSystem",
        "credentials_base64=e30K",  # base 64 for "{}"
    ),
)
def test_gcs(dest, test_case):
    dest_uri = dest.start()
    dest_uri_read = get_uri_read(dest_uri, dest)
    test_case(dest_uri, dest_uri_read)
    dest.stop()


@pytest.mark.parametrize(
    "dest", list(DESTINATIONS.values()), ids=list(DESTINATIONS.keys())
)
@pytest.mark.parametrize(
    "test_case",
    fs_test_cases(
        "s3",
        "s3fs.S3FileSystem",
        "access_key_id=KEY&secret_access_key=SECRET",
    ),
)
def test_s3(dest, test_case):
    dest_uri = dest.start()
    dest_uri_read = get_uri_read(dest_uri, dest)
    test_case(dest_uri, dest_uri_read)
    dest.stop()


def applovin_test_cases() -> Iterable[Callable]:
    def missing_api_key():
        result = invoke_ingest_command(
            "applovin://",
            "publisher-report",
            "duckdb:///out.db",
            "public.publisher_report",
            print_output=False,
        )
        assert result.exit_code != 0
        assert has_exception(result.exception, MissingValueError)

    def invalid_source_table():
        result = invoke_ingest_command(
            "applovin://?api_key=123",
            "unknown-report",
            "duckdb:///out.db",
            "public.unknown_report",
            print_output=False,
        )
        assert result.exit_code != 0
        assert has_exception(result.exception, UnsupportedResourceError)

    return [
        missing_api_key,
        invalid_source_table,
    ]


@pytest.mark.parametrize("testcase", applovin_test_cases())
def test_applovin_source(testcase):
    testcase()


def frankfurter_test_cases() -> Iterable[Callable]:
    def invalid_source_table(dest_uri, dest_uri_read):
        schema = f"testschema_frankfurter_{get_random_string(5)}"
        dest_table = f"{schema}.frankfurter_{get_random_string(5)}"
        result = invoke_ingest_command(
            "frankfurter://",
            "invalid table",
            dest_uri,
            dest_table,
            print_output=False,
        )
        assert result.exit_code != 0
        assert has_exception(result.exception, UnsupportedResourceError)

    def interval_start_does_not_exceed_interval_end(dest_uri, dest_uri_read):
        schema = f"testschema_frankfurter_{get_random_string(5)}"
        dest_table = f"{schema}.frankfurter_{get_random_string(5)}"
        result = invoke_ingest_command(
            "frankfurter://",
            "exchange_rates",
            dest_uri,
            dest_table,
            interval_start="2025-04-11",
            interval_end="2025-04-10",
            print_output=False,
        )
        assert result.exit_code != 0
        assert has_exception(result.exception, ValueError)
        assert "Interval-end cannot be before interval-start." in str(result.exception)

    def interval_start_can_equal_interval_end(dest_uri, dest_uri_read):
        if dest_uri.startswith("cratedb://"):
            pytest.skip(
                "CrateDB support for 'merge' strategy pending, "
                "see https://github.com/crate/dlt-cratedb/issues/14"
            )
        schema = f"testschema_frankfurter_{get_random_string(5)}"
        dest_table = f"{schema}.frankfurter_{get_random_string(5)}"
        result = invoke_ingest_command(
            "frankfurter://",
            "exchange_rates",
            dest_uri,
            dest_table,
            interval_start="2025-04-10",
            interval_end="2025-04-10",
            print_output=False,
        )
        assert result.exit_code == 0

    def interval_start_does_not_exceed_current_date(dest_uri, dest_uri_read):
        schema = f"testschema_frankfurter_{get_random_string(5)}"
        dest_table = f"{schema}.frankfurter_{get_random_string(5)}"
        start_date = pendulum.now().add(days=1).format("YYYY-MM-DD")
        result = invoke_ingest_command(
            "frankfurter://",
            "exchange_rates",
            dest_uri,
            dest_table,
            interval_start=start_date,
            print_output=False,
        )
        assert result.exit_code != 0
        assert has_exception(result.exception, ValueError)
        assert "Interval-start cannot be in the future." in str(result.exception)

    def interval_end_does_not_exceed_current_date(dest_uri, dest_uri_read):
        schema = f"testschema_frankfurter_{get_random_string(5)}"
        dest_table = f"{schema}.frankfurter_{get_random_string(5)}"
        start_date = pendulum.now().subtract(days=1).format("YYYY-MM-DD")
        end_date = pendulum.now().add(days=1).format("YYYY-MM-DD")
        result = invoke_ingest_command(
            "frankfurter://",
            "exchange_rates",
            dest_uri,
            dest_table,
            interval_start=start_date,
            interval_end=end_date,
            print_output=False,
        )
        assert result.exit_code != 0
        assert has_exception(result.exception, ValueError)
        assert "Interval-end cannot be in the future." in str(result.exception)

    def exchange_rate_on_specific_date(dest_uri, dest_uri_read):
        if dest_uri.startswith("cratedb://"):
            pytest.skip(
                "CrateDB support for 'merge' strategy pending, "
                "see https://github.com/crate/dlt-cratedb/issues/14"
            )
        schema = f"testschema_frankfurter_{get_random_string(5)}"
        dest_table = f"{schema}.frankfurter_{get_random_string(5)}"
        start_date = "2025-01-03"
        end_date = "2025-01-03"
        result = invoke_ingest_command(
            "frankfurter://?base=EUR",
            "exchange_rates",
            dest_uri,
            dest_table,
            interval_start=start_date,
            interval_end=end_date,
            print_output=False,
        )
        assert result.exit_code == 0, f"Ingestion failed: {result.output}"

        dest_engine = sqlalchemy.create_engine(dest_uri_read)

        query = f"SELECT rate FROM {dest_table} WHERE currency_code = 'GBP'"
        with dest_engine.connect() as conn:
            if dest_engine.dialect.name == "crate":
                conn.execute(f"REFRESH TABLE {dest_table}")
            rows = conn.execute(query).fetchall()
        dest_engine.dispose()

        # Assert that the rate for GBP is 0.82993
        assert len(rows) > 0, "No data found for GBP"
        assert abs(rows[0][0] - 0.82993) <= 1e-6, (
            f"Expected rate 0.82993, but got {rows[0][0]}"
        )

    return [
        invalid_source_table,
        interval_start_does_not_exceed_interval_end,
        interval_start_can_equal_interval_end,
        interval_start_does_not_exceed_current_date,
        interval_end_does_not_exceed_current_date,
        exchange_rate_on_specific_date,
    ]


@pytest.mark.parametrize(
    "dest", list(DESTINATIONS.values()), ids=list(DESTINATIONS.keys())
)
@pytest.mark.parametrize("test_case", frankfurter_test_cases())
def test_frankfurter(dest, test_case):
    dest_uri = dest.start()
    dest_uri_read = get_uri_read(dest_uri, dest)
    test_case(dest_uri, dest_uri_read)
    dest.stop()


def test_version_cmd():
    """
    This should always be 0.0.0-dev.
    """
    from ingestr.src.version import __version__

    msg = """
    You maybe have commited ingestr/src/buildinfo.py to git.
    Remove it to fix this error.
    """

    assert __version__ == "0.0.0-dev", msg


@pytest.mark.parametrize("source", [mysqlDocker], ids=["mysql8"])
@pytest.mark.parametrize(
    "dest", list(DESTINATIONS.values()), ids=list(DESTINATIONS.keys())
)
def test_mysql_zero_dates(source, dest):
    source_uri = source.start()
    dest_uri = dest.start()

    schema_rand_prefix = f"testschema_mysql_zero_dates_{get_random_string(5)}"
    try:
        shutil.rmtree(get_abs_path("../pipeline_data"))
    except Exception:
        pass

    source_engine = sqlalchemy.create_engine(source_uri)
    with source_engine.begin() as conn:
        conn.execute(f"DROP SCHEMA IF EXISTS {schema_rand_prefix}")
        conn.execute(f"CREATE SCHEMA {schema_rand_prefix}")
        conn.execute(
            f"""
            CREATE TABLE {schema_rand_prefix}.input (
                name VARCHAR(255),
                created_at DATETIME
            );"""
        )
        conn.execute(
            f"INSERT INTO {schema_rand_prefix}.input VALUES ('Row 1', null), ('Row 2', '2024-01-01 12:00:00'), ('Row 3', null), ('Row 4', '2025-04-05 08:30:00'), ('Row 5', null)"
        )

        conn.execute("SET sql_mode = '';")

        # this is the crucial step of this test: once the field becomes non-nullable, MySQL starts returning `0000-00-00 00:00:00` for empty dates.
        conn.execute(
            f"ALTER TABLE {schema_rand_prefix}.input MODIFY created_at DATETIME NOT NULL"
        )

        res = conn.execute(
            f"select count(*) from {schema_rand_prefix}.input"
        ).fetchall()
        assert res[0][0] == 5
    source_engine.dispose()

    result = invoke_ingest_command(
        source_uri,
        f"{schema_rand_prefix}.input",
        dest_uri,
        f"{schema_rand_prefix}.output",
        sql_backend="sqlalchemy",
    )

    assert result.exit_code == 0

    dest_uri_read = get_uri_read(dest_uri, dest)
    dest_engine = sqlalchemy.create_engine(dest_uri_read)
    # CrateDB needs an explicit flush to make data available for reads immediately.
    if dest_engine.dialect.name == "crate":
        dest_engine.execute(f"REFRESH TABLE {schema_rand_prefix}.output")
    res = dest_engine.execute(f"select * from {schema_rand_prefix}.output").fetchall()
    dest_engine.dispose()

    # assert there are no new rows, since DBs like DuckDB accept NULL and dlt adds a separate string column for the value `0000-00-00 00:00:00`
    # we want 4 columns: name, created_at, _dlt_load_id, _dlt_id
    assert len(res[0]) == 4

    # import pdb; pdb.set_trace()

    res = [
        (
            row[0],
            (
                row[1].astimezone(timezone.utc).strftime("%Y-%m-%d %H:%M:%S")
                if isinstance(row[1], datetime)
                else row[1]
            ),
        )
        for row in res
    ]

    assert len(res) == 5
    if dest_uri.startswith("cratedb://"):
        assert ("Row 1", 0) in res
        assert ("Row 2", 1704110400000) in res
        assert ("Row 3", 0) in res
        assert ("Row 4", 1743841800000) in res
        assert ("Row 5", 0) in res
    else:
        assert res[0] == ("Row 1", "1970-01-01 00:00:00")
        assert res[1] == ("Row 2", "2024-01-01 12:00:00")
        assert res[2] == ("Row 3", "1970-01-01 00:00:00")
        assert res[3] == ("Row 4", "2025-04-05 08:30:00")
        assert res[4] == ("Row 5", "1970-01-01 00:00:00")

    # Clean up
    source.stop()
    dest.stop()


def appsflyer_test_cases():
    source_uri = "appsflyer://?api_key=" + os.environ.get(
        "INGESTR_TEST_APPSFLYER_TOKEN", ""
    )

    def creatives(dest_uri: str, dest_uri_read: str):
        schema_rand_prefix = f"testschema_appsflyer_{get_random_string(5)}"
        result = invoke_ingest_command(
            source_uri,
            "creatives",
            dest_uri,
            f"{schema_rand_prefix}.creatives",
            interval_start="2025-04-01",
            interval_end="2025-04-15",
            print_output=False,
        )
        assert result.exit_code == 0

        with sqlalchemy.create_engine(dest_uri_read).connect() as conn:
            # CrateDB needs an explicit flush to make data available for reads immediately.
            if conn.dialect.name == "crate":
                conn.execute(f"REFRESH TABLE {schema_rand_prefix}.creatives")
            res = conn.execute(
                f"select * from {schema_rand_prefix}.creatives"
            ).fetchall()
            assert len(res) > 0
            columns = [
                col[0]
                for col in conn.execute(
                    f"select * from {schema_rand_prefix}.creatives limit 0"
                ).cursor.description
            ]
            expected_columns = [
                "_dlt_load_id",
                "_dlt_id",
                "campaign",
                "geo",
                "app_id",
                "install_time",
                "adset_id",
                "adset",
                "ad_id",
                "impressions",
                "clicks",
                "installs",
                "cost",
                "revenue",
                "average_ecpi",
                "loyal_users",
                "uninstalls",
                "roi",
            ]
            assert sorted(columns) == sorted(expected_columns)

    def campaigns(dest_uri: str, dest_uri_read: str):
        schema_rand_prefix = f"testschema_appsflyer_{get_random_string(5)}"
        result = invoke_ingest_command(
            source_uri,
            "campaigns",
            dest_uri,
            f"{schema_rand_prefix}.campaigns",
            interval_start="2025-04-01",
            interval_end="2025-04-15",
            print_output=False,
        )
        assert result.exit_code == 0

        with sqlalchemy.create_engine(dest_uri_read).connect() as conn:
            # CrateDB needs an explicit flush to make data available for reads immediately.
            if conn.dialect.name == "crate":
                conn.execute(f"REFRESH TABLE {schema_rand_prefix}.campaigns")
            res = conn.execute(
                f"select * from {schema_rand_prefix}.campaigns"
            ).fetchall()
            assert len(res) > 0
            columns = [
                col[0]
                for col in conn.execute(
                    f"select * from {schema_rand_prefix}.campaigns limit 0"
                ).cursor.description
            ]
            expected_columns = [
                "_dlt_load_id",
                "_dlt_id",
                "campaign",
                "geo",
                "app_id",
                "install_time",
                "impressions",
                "clicks",
                "installs",
                "cost",
                "revenue",
                "average_ecpi",
                "loyal_users",
                "uninstalls",
                "roi",
                "cohort_day_14_revenue_per_user",
                "cohort_day_14_total_revenue_per_user",
                "cohort_day_1_revenue_per_user",
                "cohort_day_1_total_revenue_per_user",
                "cohort_day_21_revenue_per_user",
                "cohort_day_21_total_revenue_per_user",
                "cohort_day_3_revenue_per_user",
                "cohort_day_3_total_revenue_per_user",
                "cohort_day_7_revenue_per_user",
                "cohort_day_7_total_revenue_per_user",
                "retention_day_7",
            ]
            assert sorted(columns) == sorted(expected_columns)

    def custom(dest_uri: str, dest_uri_read: str):
        schema_rand_prefix = f"testschema_appsflyer_{get_random_string(5)}"
        result = invoke_ingest_command(
            source_uri,
            "custom:c,geo,app_id,install_time:impressions,clicks,installs,cost,revenue,average_ecpi,loyal_users",
            dest_uri,
            f"{schema_rand_prefix}.custom",
            interval_start="2025-04-01",
            interval_end="2025-04-15",
            print_output=False,
        )
        assert result.exit_code == 0

        with sqlalchemy.create_engine(dest_uri_read).connect() as conn:
            # CrateDB needs an explicit flush to make data available for reads immediately.
            if conn.dialect.name == "crate":
                conn.execute(f"REFRESH TABLE {schema_rand_prefix}.custom")
            res = conn.execute(f"select * from {schema_rand_prefix}.custom").fetchall()
            assert len(res) > 0
            columns = [
                col[0]
                for col in conn.execute(
                    f"select * from {schema_rand_prefix}.custom limit 0"
                ).cursor.description
            ]
            expected_columns = [
                "_dlt_load_id",
                "_dlt_id",
                "campaign",
                "geo",
                "app_id",
                "install_time",
                "impressions",
                "clicks",
                "installs",
                "cost",
                "revenue",
                "average_ecpi",
                "loyal_users",
            ]
            assert sorted(columns) == sorted(expected_columns)

    return [campaigns, creatives, custom]


@pytest.mark.skipif(
    not os.environ.get("INGESTR_TEST_APPSFLYER_TOKEN"),
    reason="INGESTR_TEST_APPSFLYER_TOKEN environment variable is not set",
)
@pytest.mark.parametrize("testcase", appsflyer_test_cases())
@pytest.mark.parametrize(
    "dest", list(DESTINATIONS.values()), ids=list(DESTINATIONS.keys())
)
def test_appsflyer_source(testcase, dest):
    dest_uri = dest.start()
    dest_uri_read = get_uri_read(dest_uri, dest)
    testcase(dest_uri, dest_uri_read)
    dest.stop()


def airtable_test_cases():
    def table_with_base_id(dest_uri: str, dest_uri_read: str):
        source_uri = "airtable://?access_token=" + os.environ.get(
            "INGESTR_TEST_AIRTABLE_TOKEN", ""
        )
        source_table = os.environ.get("INGESTR_TEST_AIRTABLE_TABLE", "")
        schema_rand_prefix = f"testschema_airtable_{get_random_string(5)}"
        dest_table = f"{schema_rand_prefix}.output_{get_random_string(5)}"
        result = invoke_ingest_command(
            source_uri,
            source_table,
            dest_uri,
            dest_table,
            print_output=False,
        )
        if result.exit_code != 0:
            traceback.print_exception(*result.exc_info)

        assert result.exit_code == 0

        with sqlalchemy.create_engine(dest_uri_read).connect() as conn:
            # CrateDB needs an explicit flush to make data available for reads immediately.
            if conn.dialect.name == "crate":
                conn.execute(f"REFRESH TABLE {dest_table}")
            res = conn.execute(f"select count(*) from {dest_table}").fetchall()
            assert len(res) > 0
            assert res[0][0] > 0

    return [table_with_base_id]


@pytest.mark.skipif(
    not os.environ.get("INGESTR_TEST_AIRTABLE_TOKEN")
    or not os.environ.get("INGESTR_TEST_AIRTABLE_TABLE"),
    reason="INGESTR_TEST_AIRTABLE_TOKEN and INGESTR_TEST_AIRTABLE_TABLE environment variables are not set",
)
@pytest.mark.parametrize("testcase", airtable_test_cases())
@pytest.mark.parametrize(
    "dest", list(DESTINATIONS.values()), ids=list(DESTINATIONS.keys())
)
def test_airtable_source(testcase, dest):
    dest_uri = dest.start()
    dest_uri_read = get_uri_read(dest_uri, dest)
    testcase(dest_uri, dest_uri_read)
    dest.stop()


def pp(x):
    import sys

    print(x, file=sys.stderr)


@pytest.mark.parametrize(
    "dest", list(DESTINATIONS.values()), ids=list(DESTINATIONS.keys())
)
def test_couchbase_source_local(dest):
    """
    Test Couchbase source with local containerized Couchbase instance.

    NOTE: This test requires local Couchbase Server to be stopped first,
    as it uses 1:1 port mapping (8091, 11210, etc.) to avoid SDK connection issues.
    """
    couchbase = CouchbaseContainer(COUCHBASE_IMAGE)
    couchbase.start()

    # Insert test documents
    test_documents = [
        {
            "id": 1,
            "name": "Document 1",
            "nested_parent": {
                "key1": "value1",
                "key2": {"nested1": "value1"},
                "key3": [{"nested3": "value1"}],
            },
            "key4": ["value1", "value2", "value3"],
            "value": 100,
        },
        {
            "id": 2,
            "name": "Document 2",
            "nested_parent": {
                "key1": "value2",
                "key2": {"nested1": "value2"},
                "key3": [{"nested3": "value2"}],
            },
            "key4": ["value1", "value2", "value3"],
            "value": 200,
        },
        {
            "id": 3,
            "name": "Document 3",
            "nested_parent": {
                "key1": "value3",
                "key2": {"nested1": "value3"},
                "key3": [{"nested3": "value3"}],
            },
            "key4": ["value1", "value2", "value3"],
            "value": 300,
        },
    ]

    couchbase.insert_documents(test_documents)

    dest_uri = dest.start()

    try:
        # Build source URI without bucket (bucket will be in table name)
        source_uri = couchbase.get_connection_url()
        source_table = f"{couchbase.bucket_name}.{couchbase.scope_name}.{couchbase.collection_name}"

        result = invoke_ingest_command(
            source_uri,
            source_table,
            dest_uri,
            "raw.test_couchbase_collection",
        )

        assert result.exit_code == 0, (
            f"Command failed with exit code {result.exit_code}"
        )

        with sqlalchemy.create_engine(dest_uri).connect() as conn:
            res = conn.execute(
                "select * from raw.test_couchbase_collection order by id"
            ).fetchall()

            assert len(res) == 3, f"Expected 3 documents, got {len(res)}"

            # Verify documents were ingested correctly
            # Check essential fields (id, name, value, and at least one nested field)
            ids = [row[0] for row in res]
            names = [row[1] for row in res]
            values = [row[5] for row in res]  # value column

            assert ids == [1, 2, 3], f"Expected ids [1, 2, 3], got {ids}"
            assert names == [
                "Document 1",
                "Document 2",
                "Document 3",
            ], f"Expected names, got {names}"
            assert values == [
                100,
                200,
                300,
            ], f"Expected values [100, 200, 300], got {values}"

            # Check that nested_parent__key1 was flattened correctly
            nested_values = [row[2] for row in res]
            assert nested_values == [
                "value1",
                "value2",
                "value3",
            ], f"Expected nested values, got {nested_values}"
    finally:
        dest.stop()
        couchbase.stop()


@pytest.mark.parametrize(
    "dest", list(DESTINATIONS.values()), ids=list(DESTINATIONS.keys())
)
def test_mongodb_source(dest):
    if isinstance(dest, CrateDbDockerImage):
        pytest.skip(
            "CrateDB is not supported for this test, "
            "see https://github.com/crate-workbench/ingestr/issues/5"
        )

    mongo = MongoDbContainer("mongo:7.0.7")
    mongo.start()

    db = mongo.get_connection_client()
    test_collection = db.test_db.test_collection
    test_collection.insert_many(
        [
            {
                "id": 1,
                "name": "Document 1",
                "nested_parent": {
                    "key1": "value1",
                    "key2": {"nested1": "value1"},
                    "key3": [{"nested3": "value1"}],
                },
                "key4": ["value1", "value2", "value3"],
                "value": 100,
            },
            {
                "id": 2,
                "name": "Document 2",
                "nested_parent": {
                    "key1": "value2",
                    "key2": {"nested1": "value2"},
                    "key3": [{"nested3": "value2"}],
                },
                "key4": ["value1", "value2", "value3"],
                "value": 200,
            },
            {
                "id": 3,
                "name": "Document 3",
                "nested_parent": {
                    "key1": "value3",
                    "key2": {"nested1": "value3"},
                    "key3": [{"nested3": "value3"}],
                },
                "key4": ["value1", "value2", "value3"],
                "value": 300,
            },
            {
                "id": 4,
                "name": "Document 4",
                "nested_parent": {
                    "key1": "value4",
                    "key2": {"nested1": "value4"},
                    "key3": [{"nested3": "value4"}],
                },
                "key4": ["value1", "value2", "value3"],
                "value": 400,
            },
            {
                "id": 5,
                "name": "Document 5",
                "nested_parent": {
                    "key1": "value5",
                    "key2": {"nested1": "value5"},
                    "key3": [{"nested3": "value5"}],
                },
                "key4": ["value1", "value2", "value3"],
                "value": 500,
            },
        ]
    )

    dest_uri = dest.start()
    dest_uri_read = get_uri_read(dest_uri, dest)

    try:
        invoke_ingest_command(
            mongo.get_connection_url(),
            "test_db.test_collection",
            dest_uri,
            "raw.test_collection",
        )

        with sqlalchemy.create_engine(dest_uri_read).connect() as conn:
            # CrateDB needs an explicit flush to make data available for reads immediately.
            if conn.dialect.name == "crate":
                conn.execute("REFRESH TABLE raw.test_collection")
            res = conn.execute(
                "select id, name, nested_parent__key1, nested_parent__key2, nested_parent__key3, key4, value from raw.test_collection"
            ).fetchall()

            assert len(res) == 5

            # convert string to json if needed. this is a particular case for Clickhouse which does not have json types by default.
            res = [
                (
                    row[0],
                    row[1],
                    row[2],
                    json.loads(row[3]) if isinstance(row[3], str) else row[3],
                    json.loads(row[4]) if isinstance(row[4], str) else row[4],
                    json.loads(row[5]) if isinstance(row[5], str) else row[5],
                    row[6],
                )
                for row in res
            ]

            assert res[0] == (
                1,
                "Document 1",
                "value1",
                {"nested1": "value1"},
                [{"nested3": "value1"}],
                ["value1", "value2", "value3"],
                100,
            )
            assert res[1] == (
                2,
                "Document 2",
                "value2",
                {"nested1": "value2"},
                [{"nested3": "value2"}],
                ["value1", "value2", "value3"],
                200,
            )
            assert res[2] == (
                3,
                "Document 3",
                "value3",
                {"nested1": "value3"},
                [{"nested3": "value3"}],
                ["value1", "value2", "value3"],
                300,
            )
            assert res[3] == (
                4,
                "Document 4",
                "value4",
                {"nested1": "value4"},
                [{"nested3": "value4"}],
                ["value1", "value2", "value3"],
                400,
            )
            assert res[4] == (
                5,
                "Document 5",
                "value5",
                {"nested1": "value5"},
                [{"nested3": "value5"}],
                ["value1", "value2", "value3"],
                500,
            )
    finally:
        dest.stop()
        mongo.stop()


def mongodb_custom_query_test_cases():
    def simple_filtering_query(dest_uri: str):
        """Test simple aggregation query with filtering"""
        mongo = MongoDbContainer("mongo:7.0.7")
        mongo.start()

        try:
            db = mongo.get_connection_client()
            test_collection = db.test_db.events

            # Insert test data
            test_collection.insert_many(
                [
                    {
                        "event_id": 1,
                        "event_type": "login",
                        "user_id": "user1",
                        "status": "success",
                        "value": 100,
                    },
                    {
                        "event_id": 2,
                        "event_type": "purchase",
                        "user_id": "user1",
                        "status": "success",
                        "value": 250,
                    },
                    {
                        "event_id": 3,
                        "event_type": "login",
                        "user_id": "user2",
                        "status": "success",
                        "value": 150,
                    },
                    {
                        "event_id": 4,
                        "event_type": "purchase",
                        "user_id": "user2",
                        "status": "failed",
                        "value": 300,
                    },
                    {
                        "event_id": 5,
                        "event_type": "logout",
                        "user_id": "user1",
                        "status": "success",
                        "value": 50,
                    },
                ]
            )

            # Test simple filtering query
            custom_query = '[{"$match": {"status": "success"}}, {"$project": {"_id": 1, "event_id": 1, "event_type": 1, "user_id": 1, "value": 1}}]'
            schema_rand_prefix = f"testschema_mongo_filter_{get_random_string(5)}"

            result = invoke_ingest_command(
                mongo.get_connection_url(),
                f"test_db.events:{custom_query}",
                dest_uri,
                f"{schema_rand_prefix}.events_success",
            )

            assert result.exit_code == 0

            with sqlalchemy.create_engine(dest_uri).connect() as conn:
                res = conn.execute(
                    f"select event_id, event_type, user_id, value from {schema_rand_prefix}.events_success order by event_id"
                ).fetchall()

                assert len(res) == 4  # Only successful events
                assert res[0] == (1, "login", "user1", 100)
                assert res[1] == (2, "purchase", "user1", 250)
                assert res[2] == (3, "login", "user2", 150)
                assert res[3] == (5, "logout", "user1", 50)

        finally:
            mongo.stop()

    def aggregation_with_grouping(dest_uri: str):
        """Test aggregation query with grouping operations"""
        mongo = MongoDbContainer("mongo:7.0.7")
        mongo.start()

        try:
            db = mongo.get_connection_client()
            test_collection = db.test_db.events

            # Insert test data
            test_collection.insert_many(
                [
                    {
                        "event_id": 1,
                        "event_type": "login",
                        "user_id": "user1",
                        "status": "success",
                        "value": 100,
                    },
                    {
                        "event_id": 2,
                        "event_type": "purchase",
                        "user_id": "user1",
                        "status": "success",
                        "value": 250,
                    },
                    {
                        "event_id": 3,
                        "event_type": "login",
                        "user_id": "user2",
                        "status": "success",
                        "value": 150,
                    },
                    {
                        "event_id": 4,
                        "event_type": "purchase",
                        "user_id": "user2",
                        "status": "failed",
                        "value": 300,
                    },
                    {
                        "event_id": 5,
                        "event_type": "logout",
                        "user_id": "user1",
                        "status": "success",
                        "value": 50,
                    },
                ]
            )

            # Test aggregation with grouping
            group_query = '[{"$match": {"status": "success"}}, {"$group": {"_id": "$user_id", "total_value": {"$sum": "$value"}, "event_count": {"$sum": 1}}}]'
            schema_rand_prefix = f"testschema_mongo_group_{get_random_string(5)}"

            result = invoke_ingest_command(
                mongo.get_connection_url(),
                f"test_db.events:{group_query}",
                dest_uri,
                f"{schema_rand_prefix}.user_stats",
            )

            assert result.exit_code == 0

            with sqlalchemy.create_engine(dest_uri).connect() as conn:
                res = conn.execute(
                    f"select _id, total_value, event_count from {schema_rand_prefix}.user_stats order by _id"
                ).fetchall()

                assert len(res) == 2
                assert res[0] == (
                    "user1",
                    400,
                    3,
                )  # user1: 100 + 250 + 50 = 400, 3 events
                assert res[1] == (
                    "user2",
                    150,
                    1,
                )  # user2: only 150 from login, 1 event

        finally:
            mongo.stop()

    def incremental_with_interval_placeholders(dest_uri: str):
        """Test incremental load with interval placeholders"""
        mongo = MongoDbContainer("mongo:7.0.7")
        mongo.start()

        try:
            db = mongo.get_connection_client()
            test_collection = db.test_db.events

            # Insert test data with timestamps
            test_collection.insert_many(
                [
                    {
                        "event_id": 1,
                        "event_type": "login",
                        "user_id": "user1",
                        "timestamp": datetime(
                            2024, 1, 1, 10, 0, 0, tzinfo=timezone.utc
                        ),
                        "status": "success",
                        "value": 100,
                    },
                    {
                        "event_id": 2,
                        "event_type": "purchase",
                        "user_id": "user1",
                        "timestamp": datetime(
                            2024, 1, 1, 11, 0, 0, tzinfo=timezone.utc
                        ),
                        "status": "success",
                        "value": 250,
                    },
                    {
                        "event_id": 3,
                        "event_type": "login",
                        "user_id": "user2",
                        "timestamp": datetime(2024, 1, 2, 9, 0, 0, tzinfo=timezone.utc),
                        "status": "success",
                        "value": 150,
                    },
                    {
                        "event_id": 4,
                        "event_type": "purchase",
                        "user_id": "user2",
                        "timestamp": datetime(
                            2024, 1, 2, 10, 0, 0, tzinfo=timezone.utc
                        ),
                        "status": "failed",
                        "value": 300,
                    },
                ]
            )

            # Test incremental load with interval placeholders
            incremental_query = '[{"$match": {"timestamp": {"$gte": ":interval_start", "$lt": ":interval_end"}, "status": "success"}}, {"$project": {"_id": 1, "event_id": 1, "event_type": 1, "user_id": 1, "timestamp": 1, "value": 1}}]'
            schema_rand_prefix = f"testschema_mongo_incr_{get_random_string(5)}"

            result = invoke_ingest_command(
                mongo.get_connection_url(),
                f"test_db.events:{incremental_query}",
                dest_uri,
                f"{schema_rand_prefix}.events_incremental",
                inc_strategy="append",
                inc_key="timestamp",
                interval_start="2024-01-01T00:00:00+00:00",
                interval_end="2024-01-02T00:00:00+00:00",
            )

            assert result.exit_code == 0

            with sqlalchemy.create_engine(dest_uri).connect() as conn:
                res = conn.execute(
                    f"select event_id, event_type, user_id, value from {schema_rand_prefix}.events_incremental order by event_id"
                ).fetchall()

                # Should only get events from 2024-01-01 (events 1 and 2)
                assert len(res) == 2
                assert res[0] == (1, "login", "user1", 100)
                assert res[1] == (2, "purchase", "user1", 250)

        finally:
            mongo.stop()

    def incremental_multiple_days(dest_uri: str):
        """Test incremental load across multiple days"""
        mongo = MongoDbContainer("mongo:7.0.7")
        mongo.start()

        try:
            db = mongo.get_connection_client()
            test_collection = db.test_db.events

            # Insert test data with timestamps
            test_collection.insert_many(
                [
                    {
                        "event_id": 1,
                        "event_type": "login",
                        "user_id": "user1",
                        "timestamp": datetime(
                            2024, 1, 1, 10, 0, 0, tzinfo=timezone.utc
                        ),
                        "status": "success",
                        "value": 100,
                    },
                    {
                        "event_id": 2,
                        "event_type": "purchase",
                        "user_id": "user1",
                        "timestamp": datetime(
                            2024, 1, 1, 11, 0, 0, tzinfo=timezone.utc
                        ),
                        "status": "success",
                        "value": 250,
                    },
                    {
                        "event_id": 3,
                        "event_type": "login",
                        "user_id": "user2",
                        "timestamp": datetime(2024, 1, 2, 9, 0, 0, tzinfo=timezone.utc),
                        "status": "success",
                        "value": 150,
                    },
                    {
                        "event_id": 4,
                        "event_type": "purchase",
                        "user_id": "user2",
                        "timestamp": datetime(
                            2024, 1, 2, 10, 0, 0, tzinfo=timezone.utc
                        ),
                        "status": "failed",
                        "value": 300,
                    },
                ]
            )

            incremental_query = '[{"$match": {"timestamp": {"$gte": ":interval_start", "$lt": ":interval_end"}, "status": "success"}}, {"$project": {"_id": 1, "event_id": 1, "event_type": 1, "user_id": 1, "timestamp": 1, "value": 1}}]'
            schema_rand_prefix = f"testschema_mongo_multi_{get_random_string(5)}"

            # First day
            result = invoke_ingest_command(
                mongo.get_connection_url(),
                f"test_db.events:{incremental_query}",
                dest_uri,
                f"{schema_rand_prefix}.events_multi",
                inc_strategy="append",
                inc_key="timestamp",
                interval_start="2024-01-01T00:00:00+00:00",
                interval_end="2024-01-02T00:00:00+00:00",
            )

            assert result.exit_code == 0

            # Second day
            result = invoke_ingest_command(
                mongo.get_connection_url(),
                f"test_db.events:{incremental_query}",
                dest_uri,
                f"{schema_rand_prefix}.events_multi",
                inc_strategy="append",
                inc_key="timestamp",
                interval_start="2024-01-02T00:00:00+00:00",
                interval_end="2024-01-03T00:00:00+00:00",
            )

            assert result.exit_code == 0

            with sqlalchemy.create_engine(dest_uri).connect() as conn:
                res = conn.execute(
                    f"select event_id, event_type, user_id, value from {schema_rand_prefix}.events_multi order by event_id"
                ).fetchall()

                # Should have events from both days (events 1, 2, and 3)
                assert len(res) == 3
                assert res[0] == (1, "login", "user1", 100)
                assert res[1] == (2, "purchase", "user1", 250)
                assert res[2] == (3, "login", "user2", 150)

        finally:
            mongo.stop()

    return [
        simple_filtering_query,
        aggregation_with_grouping,
        incremental_with_interval_placeholders,
        incremental_multiple_days,
    ]


@pytest.mark.parametrize("testcase", mongodb_custom_query_test_cases())
@pytest.mark.parametrize(
    "dest", list(DESTINATIONS.values()), ids=list(DESTINATIONS.keys())
)
def test_mongodb_custom_query(testcase, dest):
    """Test MongoDB custom aggregation queries"""
    testcase(dest.start())
    dest.stop()


def test_s3_destination():
    # should raise an error if endpoint_url doesn't have a scheme or a host
    with pytest.raises(ValueError, match="Invalid endpoint_url"):
        S3Destination().dlt_dest(
            uri="s3://?access_key_id=KEY&secret_access_key=SECRET&endpoint_url=localhost:9000",
            dest_table="bucket/test_table",
        )


@pytest.mark.parametrize(
    "stripe_table",
    [
        "subscription",
        "customer",
        "product",
        "price",
        "event",
        "invoice",
        "charge",
        "balancetransaction",
    ],
)
def test_stripe_source_full_refresh(stripe_table):
    # Get Stripe token from environment
    stripe_token = os.environ.get("INGESTR_TEST_STRIPE_TOKEN")
    if not stripe_token:
        pytest.skip("INGESTR_TEST_STRIPE_TOKEN not set")

    # Create test database
    dbname = f"test_stripe_{stripe_table}{get_random_string(5)}.db"
    abs_db_path = get_abs_path(f"./testdata/{dbname}")
    rel_db_path_to_command = f"ingestr/testdata/{dbname}"
    uri = f"duckdb:///{rel_db_path_to_command}"

    # Run ingest command
    result = invoke_ingest_command(
        f"stripe://{stripe_table}s?api_key={stripe_token}",
        stripe_table,
        uri,
        f"raw.{stripe_table}s",
    )

    assert result.exit_code == 0

    # Verify data was loaded
    conn = duckdb.connect(abs_db_path)
    res = conn.sql(f"select count(*) from raw.{stripe_table}s").fetchone()
    assert res[0] > 0, f"No {stripe_table} records found"

    # Clean up
    conn.close()
    try:
        os.remove(abs_db_path)
    except Exception:
        pass


@pytest.mark.parametrize(
    "stripe_table", ["event", "invoice", "charge", "balancetransaction"]
)
def test_stripe_source_incremental(stripe_table):
    # Get Stripe token from environment
    stripe_token = os.environ.get("INGESTR_TEST_STRIPE_TOKEN")
    if not stripe_token:
        pytest.skip("INGESTR_TEST_STRIPE_TOKEN not set")

    # Create test database
    dbname = f"test_stripe_{stripe_table}{get_random_string(5)}.db"
    abs_db_path = get_abs_path(f"./testdata/{dbname}")
    rel_db_path_to_command = f"ingestr/testdata/{dbname}"
    uri = f"duckdb:///{rel_db_path_to_command}"

    # Run ingest command
    result = invoke_ingest_command(
        f"stripe://{stripe_table}s?api_key={stripe_token}",
        stripe_table,
        uri,
        f"raw.{stripe_table}s",
        interval_start="2025-04-01",
        interval_end="2025-05-30",
    )

    assert result.exit_code == 0

    # Verify data was loaded
    conn = duckdb.connect(abs_db_path)
    res = conn.sql(f"select count(*) from raw.{stripe_table}s").fetchone()
    assert res[0] > 0, f"No {stripe_table} records found"

    # Clean up
    conn.close()
    try:
        os.remove(abs_db_path)
    except Exception:
        pass


def trustpilot_test_case(dest_uri, dest_uri_read):
    if dest_uri.startswith("cratedb://"):
        pytest.skip(
            "CrateDB support for 'merge' strategy pending, "
            "see https://github.com/crate/dlt-cratedb/issues/14"
        )

    sample_response = {
        "links": [
            {
                "href": "<Url for the resource>",
                "method": "<Http method for the resource>",
                "rel": "<Description of the relation>",
            }
        ],
        "reviews": [
            {
                "id": 1,
                "stars": 0,
                "title": None,
                "text": None,
                "language": None,
                "createdAt": "2023-01-01T12:00:00Z",
                "experiencedAt": "2023-01-01T12:00:00Z",
                "updatedAt": "2023-01-01T12:00:00Z",
                "numberOfLikes": 0,
                "isVerified": False,
                "status": None,
                "companyReply": {
                    "text": "This is our reply.",
                    "createdAt": "2013-09-07T13:37:00",
                    "updatedAt": "2013-09-07T13:37:00",
                },
                "consumer": {
                    "displayLocation": "Frederiksberg, DK",
                    "numberOfReviews": 1,
                    "displayName": "John Doe",
                    "id": "507f191e810c19729de860ea",
                    "links": [
                        {
                            "href": "<Url for the resource>",
                            "method": "<Http method for the resource>",
                            "rel": "<Description of the relation>",
                        }
                    ],
                },
                "businessUnit": {
                    "identifyingName": "trustpilot.com",
                    "displayName": "Trustpilot",
                    "id": "507f191e810c19729de860ea",
                    "links": [
                        {
                            "href": "<Url for the resource>",
                            "method": "<Http method for the resource>",
                            "rel": "<Description of the relation>",
                        }
                    ],
                },
                "location": {
                    "id": "43f51215-a1fc-4c60-b6dd-e4afb6d7b831",
                    "name": "Pilestraede 58",
                    "urlFormattedName": "Pilestraede58",
                },
                "countsTowardsTrustScore": False,
                "countsTowardsLocationTrustScore": False,
                "links": [
                    {
                        "href": "<Url for the resource>",
                        "method": "<Http method for the resource>",
                        "rel": "<Description of the relation>",
                    }
                ],
                "reportData": {
                    "source": "Trustpilot",
                    "publicComment": "This review contains sensitive information.",
                    "createdAt": "2013-09-07T13:37:00",
                    "reasons": ["sensitiveInformation", "consumerIsCompetitor"],
                    "reason": "consumer_is_competitor",
                    "reviewVisibility": "hidden",
                },
                "complianceLabels": [None],
                "invitation": {"businessUnitId": "507f191e810c19729de860ea"},
                "businessUnitHistory": [
                    {
                        "businessUnitId": "507f191e810c19729de860ea",
                        "identifyingName": "example.com",
                        "displayName": "Example Inc.",
                        "changeDate": "2013-09-07T13:37:00",
                    }
                ],
                "reviewVerificationLevel": None,
            }
        ],
    }

    with patch("dlt.sources.helpers.requests.get") as mock_get:
        mock_response = MagicMock()
        mock_response.json.return_value = sample_response
        mock_response.raise_for_status = MagicMock()
        mock_get.return_value = mock_response

        dest_table = "trustpilot.reviews"
        source_uri = "trustpilot://<business_unit_id>?api_key=<api_key>"
        source_table = "reviews"

        result = invoke_ingest_command(
            source_uri,
            source_table,
            dest_uri,
            dest_table,
        )

        assert result.exit_code == 0

        engine = sqlalchemy.create_engine(dest_uri_read)
        with engine.connect() as conn:
            # CrateDB needs an explicit flush to make data available for reads immediately.
            if conn.dialect.name == "crate":
                conn.execute(f"REFRESH TABLE {dest_table}")
            rows = conn.execute(f"SELECT * FROM {dest_table}").fetchall()
            assert len(rows) > 0, "No data ingested into the destination"
        engine.dispose()


@pytest.mark.parametrize(
    "dest", list(DESTINATIONS.values()), ids=list(DESTINATIONS.keys())
)
def test_trustpilot(dest):
    dest_uri = dest.start()
    dest_uri_read = get_uri_read(dest_uri, dest)
    trustpilot_test_case(dest_uri, dest_uri_read)
    dest.stop()


def pinterest_test_case(dest_uri, dest_uri_read):
    sample_response = {
        "items": [
            {
                "id": "813744226420795884",
                "created_at": "2020-01-01T20:10:40-00:00",
                "link": "https://www.pinterest.com/",
                "title": "string",
                "description": "string",
                "dominant_color": "#6E7874",
                "alt_text": "string",
                "creative_type": "REGULAR",
                "board_id": "string",
                "board_section_id": "string",
                "board_owner": {"username": "string"},
                "is_owner": "false",
                "media": {
                    "media_type": "string",
                    "images": {
                        "150x150": {
                            "width": 150,
                            "height": 150,
                            "url": "https://i.pinimg.com/150x150/0d/f6/f1/0df6f1f0bfe7aaca849c1bbc3607a34b.jpg",
                        },
                        "400x300": {
                            "width": 400,
                            "height": 300,
                            "url": "https://i.pinimg.com/400x300/0d/f6/f1/0df6f1f0bfe7aaca849c1bbc3607a34b.jpg",
                        },
                        "600x": {
                            "width": 600,
                            "height": 600,
                            "url": "https://i.pinimg.com/600x/0d/f6/f1/0df6f1f0bfe7aaca849c1bbc3607a34b.jpg",
                        },
                        "1200x": {
                            "width": 1200,
                            "height": 1200,
                            "url": "https://i.pinimg.com/1200x/0d/f6/f1/0df6f1f0bfe7aaca849c1bbc3607a34b.jpg",
                        },
                    },
                },
                "parent_pin_id": "string",
                "is_standard": "false",
                "has_been_promoted": "false",
                "note": "string",
                "pin_metrics": {
                    "90d": {"pin_click": 7, "impression": 2, "clickthrough": 3},
                    "lifetime_metrics": {
                        "pin_click": 7,
                        "impression": 2,
                        "clickthrough": 3,
                        "reaction": 10,
                        "comment": 2,
                    },
                },
                "is_removable": True,
            }
        ],
        "bookmark": "string",
    }

    sample_response_last = {"items": []}

    with patch("dlt.sources.helpers.requests.Session.get") as mock_get:
        mock_response_1 = MagicMock()
        mock_response_1.json.return_value = sample_response
        mock_response_1.raise_for_status = MagicMock()

        mock_response_2 = MagicMock()
        mock_response_2.json.return_value = sample_response_last
        mock_response_2.raise_for_status = MagicMock()

        mock_get.side_effect = [mock_response_1, mock_response_2]
        dest_table = "dest.pins"
        source_uri = "pinterest://?access_token=token_123"
        source_table = "pins"

        result = invoke_ingest_command(
            source_uri,
            source_table,
            dest_uri,
            dest_table,
        )

        assert result.exit_code == 0

        engine = sqlalchemy.create_engine(dest_uri_read)
        with engine.connect() as conn:
            # CrateDB needs an explicit flush to make data available for reads immediately.
            if conn.dialect.name == "crate":
                conn.execute(f"REFRESH TABLE {dest_table}")
            rows = conn.execute(f"SELECT * FROM {dest_table}").fetchall()
            assert len(rows) > 0, "No data ingested into the destination"
        engine.dispose()


@pytest.mark.parametrize(
    "dest", list(DESTINATIONS.values()), ids=list(DESTINATIONS.keys())
)
def test_pinterest_test_case(dest):
    if isinstance(dest, CrateDbDockerImage):
        pytest.skip(
            "CrateDB support for 'merge' strategy pending, "
            "see https://github.com/crate/dlt-cratedb/issues/14"
        )
    dest_uri = dest.start()
    dest_uri_read = get_uri_read(dest_uri, dest)
    pinterest_test_case(dest_uri, dest_uri_read)
    dest.stop()


def linear_test_cases():
    # All Linear source tables
    tables = [
        "issues",
        "projects",
        "users",
        "workflow_states",
        "cycles",
        "attachments",
        "comments",
        "documents",
        "external_users",
        "initiative",
        "integrations",
        "labels",
        "organization",
        "project_updates",
        "team_memberships",
        "initiative_to_project",
        "project_milestone",
        "project_status",
    ]

    def create_table_test(table_name):
        def table_test(dest_uri: str):
            linear_api_key = os.environ.get("INGESTR_TEST_LINEAR_API_KEY", "")
            if not linear_api_key:
                pytest.skip(
                    "INGESTR_TEST_LINEAR_API_KEY environment variable is not set"
                )

            source_uri = f"linear://?api_key={linear_api_key}"
            source_table = table_name
            schema_rand_prefix = f"testschema_linear_{get_random_string(5)}"
            dest_table = f"{schema_rand_prefix}.{table_name}_{get_random_string(5)}"

            result = invoke_ingest_command(
                source_uri,
                source_table,
                dest_uri,
                dest_table,
                interval_start="2020-01-01",
                interval_end="2025-12-31",
                print_output=True,
            )

            if result.exit_code != 0:
                # Some Linear resources might not be accessible based on workspace permissions
                print(
                    f"Linear {table_name} test failed (likely permissions/access issue)"
                )
                traceback.print_exception(*result.exc_info)

            assert result.exit_code == 0

            with sqlalchemy.create_engine(dest_uri).connect() as conn:
                res = conn.execute(f"select count(*) from {dest_table}").fetchall()
                assert len(res) > 0
                count = res[0][0]
                print(f"Linear {table_name} count: {count}")

                # Special validation for users table - should have at least one user
                if table_name == "users":
                    assert count > 0, "Linear should have at least one user"

        # Set function name for pytest identification
        table_test.__name__ = f"{table_name}_table"
        return table_test

    return [create_table_test(table) for table in tables]


@pytest.mark.skipif(
    not os.environ.get("INGESTR_TEST_LINEAR_API_KEY"),
    reason="INGESTR_TEST_LINEAR_API_KEY environment variable is not set",
)
@pytest.mark.parametrize("testcase", linear_test_cases())
@pytest.mark.parametrize(
    "dest", list(DESTINATIONS.values()), ids=list(DESTINATIONS.keys())
)
def test_linear_source(testcase, dest):
    testcase(dest.start())
    dest.stop()


def jira_test_cases():
    # All Jira source tables
    tables = [
        "projects",
        "issues",
        "users",
        "issue_types",
        "statuses",
        "priorities",
        "resolutions",
    ]

    def create_table_test(table_name):
        def table_test(dest_uri: str):
            jira_base_url = os.environ.get("INGESTR_TEST_JIRA_BASE_URL", "")
            jira_email = os.environ.get("INGESTR_TEST_JIRA_EMAIL", "")
            jira_api_token = os.environ.get("INGESTR_TEST_JIRA_API_TOKEN", "")

            if not jira_base_url or not jira_email or not jira_api_token:
                pytest.skip(
                    "INGESTR_TEST_JIRA_BASE_URL, INGESTR_TEST_JIRA_EMAIL, or INGESTR_TEST_JIRA_API_TOKEN environment variables are not set"
                )

            # Extract domain from base_url (remove https:// if present)
            domain = jira_base_url.replace("https://", "").replace("http://", "")
            source_uri = (
                f"jira://{domain}?email={jira_email}&api_token={jira_api_token}"
            )
            source_table = table_name
            schema_rand_prefix = f"testschema_jira_{get_random_string(5)}"
            dest_table = f"{schema_rand_prefix}.{table_name}_{get_random_string(5)}"

            result = invoke_ingest_command(
                source_uri,
                source_table,
                dest_uri,
                dest_table,
                interval_start="2020-01-01",
                interval_end="2025-12-31",
                print_output=True,
            )

            if result.exit_code != 0:
                # Some Jira resources might not be accessible based on workspace permissions
                print(
                    f"Jira {table_name} test failed (likely permissions/access issue)"
                )
                traceback.print_exception(*result.exc_info)

            assert result.exit_code == 0

            with sqlalchemy.create_engine(dest_uri).connect() as conn:
                res = conn.execute(f"select count(*) from {dest_table}").fetchall()
                assert len(res) >= 0  # Just verify the table exists and query works
                count = res[0][0]
                print(f"Jira {table_name} count: {count}")

                # Special validation for certain tables that should always have data
                if table_name == "projects":
                    assert count > 0, "Jira should have at least one project"
                elif table_name == "issue_types":
                    assert count > 0, "Jira should have at least one issue type"
                elif table_name == "statuses":
                    assert count > 0, "Jira should have at least one status"
                # project_versions and project_components can be empty, so no assertion for them

        # Set function name for pytest identification
        table_test.__name__ = f"{table_name}_table"
        return table_test

    return [create_table_test(table) for table in tables]


@pytest.mark.skipif(
    not all(
        [
            os.environ.get("INGESTR_TEST_JIRA_BASE_URL"),
            os.environ.get("INGESTR_TEST_JIRA_EMAIL"),
            os.environ.get("INGESTR_TEST_JIRA_API_TOKEN"),
        ]
    ),
    reason="INGESTR_TEST_JIRA_BASE_URL, INGESTR_TEST_JIRA_EMAIL, or INGESTR_TEST_JIRA_API_TOKEN environment variables are not set",
)
@pytest.mark.parametrize("testcase", jira_test_cases())
@pytest.mark.parametrize(
    "dest", list(DESTINATIONS.values()), ids=list(DESTINATIONS.keys())
)
def test_jira_source(testcase, dest):
    testcase(dest.start())
    dest.stop()


def revenuecat_test_cases():
    # All RevenueCat source tables
    tables = ["projects", "customers", "products", "entitlements", "offerings"]

    def create_table_test(table_name):
        def table_test(dest_uri: str):
            revenuecat_api_key = os.environ.get("INGESTR_TEST_REVENUECAT_API_KEY", "")
            revenuecat_project_id = os.environ.get(
                "INGESTR_TEST_REVENUECAT_PROJECT_ID", ""
            )

            if not revenuecat_api_key:
                pytest.skip(
                    "INGESTR_TEST_REVENUECAT_API_KEY environment variable is not set"
                )

            # Projects table doesn't need project_id, others do
            if table_name != "projects" and not revenuecat_project_id:
                pytest.skip(
                    "INGESTR_TEST_REVENUECAT_PROJECT_ID environment variable is not set"
                )

            # Build source URI
            if table_name == "projects":
                source_uri = f"revenuecat://?api_key={revenuecat_api_key}"
            else:
                source_uri = f"revenuecat://?api_key={revenuecat_api_key}&project_id={revenuecat_project_id}"

            source_table = table_name
            schema_rand_prefix = f"testschema_revenuecat_{get_random_string(5)}"
            dest_table = f"{schema_rand_prefix}.{table_name}_{get_random_string(5)}"

            # Limit customers table to 100 records for faster testing
            yield_limit = 100 if table_name == "customers" else None

            result = invoke_ingest_command(
                source_uri,
                source_table,
                dest_uri,
                dest_table,
                yield_limit=yield_limit,
                print_output=True,
            )

            if result.exit_code != 0:
                # Some RevenueCat resources might not be accessible based on API key permissions
                print(
                    f"RevenueCat {table_name} test failed (likely permissions/access issue)"
                )
                traceback.print_exception(*result.exc_info)

            assert result.exit_code == 0

            with sqlalchemy.create_engine(dest_uri).connect() as conn:
                res = conn.execute(f"select count(*) from {dest_table}").fetchall()
                assert len(res) > 0
                count = res[0][0]
                print(f"RevenueCat {table_name} count: {count}")

                # Special validation for projects table - should have at least one project
                if table_name == "projects":
                    assert count > 0, "RevenueCat should have at least one project"

        # Set function name for pytest identification
        table_test.__name__ = f"{table_name}_table"
        return table_test

    return [create_table_test(table) for table in tables]


@pytest.mark.skipif(
    not os.environ.get("INGESTR_TEST_REVENUECAT_API_KEY"),
    reason="INGESTR_TEST_REVENUECAT_API_KEY environment variable is not set",
)
@pytest.mark.parametrize("testcase", revenuecat_test_cases())
@pytest.mark.parametrize(
    "dest", list(DESTINATIONS.values()), ids=list(DESTINATIONS.keys())
)
def test_revenuecat_source(testcase, dest):
    testcase(dest.start())
    dest.stop()


def mongodb_test_cases():
    def smoke_test(mongo):
        collection = f"smoke_test_{get_random_string(5)}"
        result = invoke_ingest_command(
            "csv://ingestr/testdata/create_replace.csv",
            "raw.input",
            mongo.get_connection_url(),
            collection,
        )
        assert result.exit_code == 0

        client = mongo.get_connection_client()
        assert client["ingestr_db"][collection].count_documents({}) == 20

    def large_insert(mongo):
        """
        Insert more than batch_size items.
        """
        DOC_COUNT = 5000
        table = pa.Table.from_pandas(
            pd.DataFrame(
                {
                    "id": range(DOC_COUNT),
                }
            )
        )
        with tempfile.NamedTemporaryFile(suffix=".arrow") as fd:
            with pa.OSFile(fd.name, "wb") as f:
                writer = ipc.new_file(f, table.schema)
                writer.write_table(table)
                writer.close()

            collection = f"large_insert_{get_random_string(5)}"
            result = invoke_ingest_command(
                f"mmap://{fd.name}",
                "raw.input",
                mongo.get_connection_url(),
                collection,
            )
            assert result.exit_code == 0

            client = mongo.get_connection_client()
            assert client["ingestr_db"][collection].count_documents({}) == DOC_COUNT

    return [
        smoke_test,
        large_insert,
    ]


@pytest.mark.parametrize("testcase", mongodb_test_cases())
def test_mongodb_dest(testcase, mongodb_server):
    testcase(mongodb_server)


@pytest.mark.skipif(
    not os.environ.get("COUCHBASE_CAPELLA_USERNAME")
    or not os.environ.get("COUCHBASE_CAPELLA_PASSWORD"),
    reason="Couchbase Capella credentials not set",
)
@pytest.mark.parametrize(
    "dest", list(DESTINATIONS.values()), ids=list(DESTINATIONS.keys())
)
def test_couchbase_capella_source(dest):
    """
    Test Couchbase Capella (cloud) as a source with bucket in URI.
    Uses SSL connection with bucket specified in URI path.

    Required environment variables:
    - COUCHBASE_CAPELLA_USERNAME
    - COUCHBASE_CAPELLA_PASSWORD
    - COUCHBASE_CAPELLA_HOST
    - COUCHBASE_CAPELLA_BUCKET
    - COUCHBASE_CAPELLA_SCOPE
    - COUCHBASE_CAPELLA_COLLECTION
    """
    username = os.environ.get("COUCHBASE_CAPELLA_USERNAME")
    password = os.environ.get("COUCHBASE_CAPELLA_PASSWORD")
    host = os.environ.get(
        "COUCHBASE_CAPELLA_HOST", "cb.8vm1qjx5nowztp08.cloud.couchbase.com"
    )
    bucket = os.environ.get("COUCHBASE_CAPELLA_BUCKET", "travel-sample")
    scope = os.environ.get("COUCHBASE_CAPELLA_SCOPE", "inventory")
    collection = os.environ.get("COUCHBASE_CAPELLA_COLLECTION", "airline")

    # Test with bucket in URI and ssl=true parameter
    source_uri = f"couchbase://{username}:{password}@{host}/{bucket}?ssl=true"
    source_table = f"{scope}.{collection}"

    dest_uri = dest.start()
    dest_table = "raw.couchbase_capella_test"

    try:
        result = invoke_ingest_command(
            source_uri,
            source_table,
            dest_uri,
            dest_table,
        )

        assert result.exit_code == 0, f"Command failed with: {result.output}"

        # Verify data was ingested
        with sqlalchemy.create_engine(dest_uri).connect() as conn:
            res = conn.execute(f"select * from {dest_table}").fetchall()
            assert len(res) > 0, "No data was ingested from Couchbase Capella"
            print(
                f"Successfully ingested {len(res)} documents from Couchbase Capella (bucket in URI)"
            )
    finally:
        dest.stop()


@pytest.mark.skipif(
    not os.environ.get("COUCHBASE_CAPELLA_USERNAME")
    or not os.environ.get("COUCHBASE_CAPELLA_PASSWORD"),
    reason="Couchbase Capella credentials not set",
)
@pytest.mark.parametrize(
    "dest", list(DESTINATIONS.values()), ids=list(DESTINATIONS.keys())
)
def test_couchbase_capella_source_without_bucket_in_uri(dest):
    """
    Test Couchbase Capella with bucket specified in table name instead of URI.
    Uses SSL connection with bucket as part of the table identifier.
    """
    username = os.environ.get("COUCHBASE_CAPELLA_USERNAME")
    password = os.environ.get("COUCHBASE_CAPELLA_PASSWORD")
    host = os.environ.get(
        "COUCHBASE_CAPELLA_HOST", "cb.8vm1qjx5nowztp08.cloud.couchbase.com"
    )
    bucket = os.environ.get("COUCHBASE_CAPELLA_BUCKET", "travel-sample")
    scope = os.environ.get("COUCHBASE_CAPELLA_SCOPE", "inventory")
    collection = os.environ.get("COUCHBASE_CAPELLA_COLLECTION", "airline")

    # Test without bucket in URI - bucket is part of table name
    source_uri = f"couchbase://{username}:{password}@{host}?ssl=true"
    source_table = f"{bucket}.{scope}.{collection}"

    dest_uri = dest.start()
    dest_table = "raw.couchbase_capella_test2"

    try:
        result = invoke_ingest_command(
            source_uri,
            source_table,
            dest_uri,
            dest_table,
        )

        assert result.exit_code == 0, f"Command failed with: {result.output}"

        # Verify data was ingested
        with sqlalchemy.create_engine(dest_uri).connect() as conn:
            res = conn.execute(f"select * from {dest_table}").fetchall()
            assert len(res) > 0, "No data was ingested from Couchbase Capella"
            print(
                f"Successfully ingested {len(res)} documents from Couchbase Capella (bucket in table name)"
            )
    finally:
        dest.stop()


@pytest.mark.skipif(
    not os.environ.get("COUCHBASE_SERVER_USERNAME")
    or not os.environ.get("COUCHBASE_SERVER_PASSWORD"),
    reason="Couchbase Server credentials not set",
)
@pytest.mark.parametrize(
    "dest", list(DESTINATIONS.values()), ids=list(DESTINATIONS.keys())
)
def test_couchbase_server_source(dest):
    """
    Test Couchbase Server (self-hosted) as a source.

    Required environment variables:
    - COUCHBASE_SERVER_USERNAME
    - COUCHBASE_SERVER_PASSWORD
    - COUCHBASE_SERVER_HOST (optional, defaults to localhost)
    - COUCHBASE_SERVER_BUCKET (optional, defaults to default)
    - COUCHBASE_SERVER_SCOPE (optional, defaults to _default)
    - COUCHBASE_SERVER_COLLECTION (optional, defaults to _default)
    """
    username = os.environ.get("COUCHBASE_SERVER_USERNAME")
    password = os.environ.get("COUCHBASE_SERVER_PASSWORD")
    host = os.environ.get("COUCHBASE_SERVER_HOST", "localhost")
    bucket = os.environ.get("COUCHBASE_SERVER_BUCKET", "default")
    scope = os.environ.get("COUCHBASE_SERVER_SCOPE", "_default")
    collection = os.environ.get("COUCHBASE_SERVER_COLLECTION", "_default")

    # Couchbase Server typically doesn't require SSL for local connections
    source_uri = f"couchbase://{username}:{password}@{host}/{bucket}"
    source_table = f"{scope}.{collection}"

    dest_uri = dest.start()
    dest_table = "raw.couchbase_server_test"

    try:
        result = invoke_ingest_command(
            source_uri,
            source_table,
            dest_uri,
            dest_table,
        )

        assert result.exit_code == 0, f"Command failed with: {result.output}"

        # Verify data was ingested
        with sqlalchemy.create_engine(dest_uri).connect() as conn:
            res = conn.execute(f"select * from {dest_table}").fetchall()
            assert len(res) > 0, "No data was ingested from Couchbase Server"
            print(f"Successfully ingested {len(res)} documents from Couchbase Server")
    finally:
        dest.stop()


@pytest.mark.parametrize(
    "hostaway_table",
    [
        "listings",
        "listing_fee_settings",
        "listing_pricing_settings",
        "listing_agreements",
        "cancellation_policies",
        "cancellation_policies_airbnb",
        "cancellation_policies_marriott",
        "cancellation_policies_vrbo",
        "reservations",
        "finance_fields",
        "reservation_payment_methods",
        "reservation_rental_agreements",
        "listing_calendars",
        "conversations",
        "message_templates",
        "bed_types",
        "property_types",
        "countries",
        "account_tax_settings",
        "user_groups",
        "guest_payment_charges",
        "coupons",
        "webhook_reservations",
        "tasks",
    ],
)
def test_hostaway_source_full_refresh(hostaway_table):
    api_key = os.environ.get("INGESTR_TEST_HOSTAWAY_API_KEY")
    if not api_key:
        pytest.skip("INGESTR_TEST_HOSTAWAY_API_KEY not set")

    dbname = f"test_hostaway_{hostaway_table}_{get_random_string(5)}.db"
    abs_db_path = get_abs_path(f"./testdata/{dbname}")
    rel_db_path_to_command = f"ingestr/testdata/{dbname}"
    uri = f"duckdb:///{rel_db_path_to_command}"

    result = invoke_ingest_command(
        f"hostaway://?api_key={api_key}",
        hostaway_table,
        uri,
        f"raw.{hostaway_table}",
        interval_start="2020-01-01",
        interval_end="2025-12-31",
    )

    assert result.exit_code == 0

    conn = duckdb.connect(abs_db_path)
    result = conn.sql(f"select count(*) from raw.{hostaway_table}").fetchone()
    assert result is not None

    conn.close()
    try:
        os.remove(abs_db_path)
    except Exception:
        pass


@pytest.fixture(scope="module")
def elasticsearch_container():
    """Fixture that provides an Elasticsearch container for tests."""
    with ElasticSearchContainer(
        "docker.elastic.co/elasticsearch/elasticsearch:8.11.0"
    ) as es:
        yield es


@pytest.fixture(scope="module")
def elasticsearch_container_with_auth():
    """Fixture that provides an Elasticsearch container with authentication."""
    # Use DockerContainer instead of ElasticSearchContainer to avoid auth issues in readiness check
    container = DockerContainer("docker.elastic.co/elasticsearch/elasticsearch:8.11.0")
    container.with_exposed_ports(9200)
    container.with_env("discovery.type", "single-node")
    container.with_env("xpack.security.enabled", "true")
    container.with_env("xpack.security.http.ssl.enabled", "false")
    container.with_env("ELASTIC_PASSWORD", "testpass123")
    container.with_env("transport.host", "127.0.0.1")
    container.with_env("http.host", "0.0.0.0")
    # Memory settings for CI environments
    container.with_env("ES_JAVA_OPTS", "-Xms512m -Xmx512m")
    container.with_env("bootstrap.memory_lock", "false")

    container.start()

    # Manual readiness check with auth
    host = container.get_container_host_ip()
    port = container.get_exposed_port(9200)
    url = f"http://{host}:{port}"

    # Wait for Elasticsearch to be ready (with auth)
    # Increased timeout for CI environments where containers start slower
    max_retries = 60
    last_error = None
    for i in range(max_retries):
        try:
            req = urllib.request.Request(url)
            req.add_header(
                "Authorization",
                "Basic " + base64.b64encode(b"elastic:testpass123").decode("ascii"),
            )
            response = urllib.request.urlopen(req, timeout=5)
            if response.status == 200:
                break
        except Exception as e:
            last_error = e
            if i == max_retries - 1:
                print(
                    f"Failed to connect to Elasticsearch after {max_retries} retries. Last error: {last_error}"
                )
                container.stop()
                raise
            time.sleep(2)

    # Create a simple object with get_url method for compatibility
    class ESContainer:
        def __init__(self, container, url):
            self._container = container
            self._url = url

        def get_url(self):
            return self._url

        def stop(self):
            return self._container.stop()

    es_container = ESContainer(container, url)

    try:
        yield es_container
    finally:
        container.stop()


def test_csv_to_elasticsearch(elasticsearch_container):
    """Test loading CSV data into Elasticsearch."""
    try:
        shutil.rmtree(get_abs_path("../pipeline_data"))
    except Exception:
        pass

    # Create a temporary CSV file
    csv_content = """id,name,age,city
1,Alice,30,New York
2,Bob,25,San Francisco
3,Charlie,35,Boston
"""
    with tempfile.NamedTemporaryFile(mode="w", suffix=".csv", delete=False) as f:
        f.write(csv_content)
        csv_path = f.name

    try:
        # Get Elasticsearch connection details
        es_url = elasticsearch_container.get_url()
        parsed = urlparse(es_url)
        netloc = parsed.netloc
        secure = "true" if parsed.scheme == "https" else "false"

        # Invoke ingest command
        result = invoke_ingest_command(
            f"csv://{csv_path}",
            "test_data",
            f"elasticsearch://{netloc}?secure={secure}",
            "test_index",
        )

        assert result.exit_code == 0, f"Command failed with output: {result.stdout}"

        # Verify data in Elasticsearch
        es_client = Elasticsearch([es_url])

        # Wait a bit for indexing
        es_client.indices.refresh(index="test_index")

        # Get document count
        count_result = es_client.count(index="test_index")
        assert count_result["count"] == 3

        # Get all documents
        search_result = es_client.search(
            index="test_index", body={"query": {"match_all": {}}}
        )
        docs = search_result["hits"]["hits"]

        assert len(docs) == 3

        # Verify document content
        names = sorted([doc["_source"]["name"] for doc in docs])
        assert names == ["Alice", "Bob", "Charlie"]

    finally:
        # Clean up
        os.remove(csv_path)
        try:
            shutil.rmtree(get_abs_path("../pipeline_data"))
        except Exception:
            pass


def test_csv_to_elasticsearch_with_auth(elasticsearch_container_with_auth):
    """Test loading CSV data into Elasticsearch with authentication."""
    try:
        shutil.rmtree(get_abs_path("../pipeline_data"))
    except Exception:
        pass

    # Create a temporary CSV file
    csv_content = """id,name,department
1,Alice,Engineering
2,Bob,Sales
3,Charlie,Marketing
"""
    with tempfile.NamedTemporaryFile(mode="w", suffix=".csv", delete=False) as f:
        f.write(csv_content)
        csv_path = f.name

    try:
        # Get Elasticsearch connection details
        es_url = elasticsearch_container_with_auth.get_url()
        parsed = urlparse(es_url)
        netloc = parsed.netloc
        secure = "true" if parsed.scheme == "https" else "false"

        # Invoke ingest command with auth
        result = invoke_ingest_command(
            f"csv://{csv_path}",
            "test_data",
            f"elasticsearch://elastic:testpass123@{netloc}?secure={secure}",
            "test_auth_index",
        )

        assert result.exit_code == 0, f"Command failed with output: {result.stdout}"

        # Verify data in Elasticsearch with auth
        es_client = Elasticsearch([es_url], http_auth=("elastic", "testpass123"))

        # Wait for indexing
        es_client.indices.refresh(index="test_auth_index")

        # Get document count
        count_result = es_client.count(index="test_auth_index")
        assert count_result["count"] == 3

        # Get all documents
        search_result = es_client.search(
            index="test_auth_index", body={"query": {"match_all": {}}}
        )
        docs = search_result["hits"]["hits"]

        assert len(docs) == 3

        # Verify departments
        departments = sorted([doc["_source"]["department"] for doc in docs])
        assert departments == ["Engineering", "Marketing", "Sales"]

    finally:
        # Clean up
        os.remove(csv_path)
        try:
            shutil.rmtree(get_abs_path("../pipeline_data"))
        except Exception:
            pass


def test_elasticsearch_replace_strategy(elasticsearch_container):
    """Test that replace strategy deletes existing data and replaces it."""
    try:
        shutil.rmtree(get_abs_path("../pipeline_data"))
    except Exception:
        pass

    # Get Elasticsearch connection
    es_url = elasticsearch_container.get_url()
    es_client = Elasticsearch([es_url])

    # Create index with initial data
    index_name = "replace_test_index"

    if es_client.indices.exists(index=index_name):
        es_client.indices.delete(index=index_name)

    es_client.index(
        index=index_name, id="1", document={"name": "OldData", "value": 100}
    )
    es_client.indices.refresh(index=index_name)

    # Create CSV with new data
    csv_content = """name,value
NewData1,200
NewData2,300
"""
    with tempfile.NamedTemporaryFile(mode="w", suffix=".csv", delete=False) as f:
        f.write(csv_content)
        csv_path = f.name

    try:
        # Load new data with replace strategy
        parsed = urlparse(es_url)
        netloc = parsed.netloc
        secure = "true" if parsed.scheme == "https" else "false"

        result = invoke_ingest_command(
            f"csv://{csv_path}",
            "test_data",
            f"elasticsearch://{netloc}?secure={secure}",
            index_name,
            inc_strategy="replace",
        )

        assert result.exit_code == 0

        # Verify old data is gone and new data is present
        es_client.indices.refresh(index=index_name)

        count_result = es_client.count(index=index_name)
        assert count_result["count"] == 2  # Only new data

        search_result = es_client.search(
            index=index_name, body={"query": {"match_all": {}}}
        )
        docs = search_result["hits"]["hits"]

        names = sorted([doc["_source"]["name"] for doc in docs])
        assert names == ["NewData1", "NewData2"]
        assert "OldData" not in names

    finally:
        os.remove(csv_path)
        try:
            if es_client.indices.exists(index=index_name):
                es_client.indices.delete(index=index_name)
        except Exception:
            pass
        try:
            shutil.rmtree(get_abs_path("../pipeline_data"))
        except Exception:
            pass


@pytest.mark.skipif(
    not os.getenv("ELASTICSEARCH_CLOUD_URL"),
    reason="ELASTICSEARCH_CLOUD_URL not set in environment",
)
def test_csv_to_elasticsearch_cloud():
    """Test loading CSV data into Elasticsearch Cloud."""
    try:
        shutil.rmtree(get_abs_path("../pipeline_data"))
    except Exception:
        pass

    # Get Elasticsearch Cloud URL from environment
    es_cloud_url = os.getenv("ELASTICSEARCH_CLOUD_URL")
    if not es_cloud_url:
        pytest.skip("ELASTICSEARCH_CLOUD_URL not configured")

    # Create a temporary CSV file
    csv_content = """id,name,department,salary
1,Alice,Engineering,95000
2,Bob,Sales,75000
3,Charlie,Marketing,80000
"""
    with tempfile.NamedTemporaryFile(mode="w", suffix=".csv", delete=False) as f:
        f.write(csv_content)
        csv_path = f.name

    try:
        # Invoke ingest command with Elasticsearch Cloud
        result = invoke_ingest_command(
            f"csv://{csv_path}",
            "test_data",
            es_cloud_url,
            "ingestr_test_cloud_index",
        )

        assert result.exit_code == 0, f"Command failed with output: {result.stdout}"

        # Verify data in Elasticsearch Cloud
        # Parse the URL to extract credentials
        parsed = urlparse(es_cloud_url.replace("elasticsearch://", "https://"))
        username = parsed.username
        password = parsed.password
        host = parsed.hostname
        port = parsed.port if parsed.port else 443

        es_url = f"https://{host}:{port}"
        es_client = Elasticsearch([es_url], basic_auth=(username, password))

        # Wait for indexing
        es_client.indices.refresh(index="ingestr_test_cloud_index")

        # Get document count
        count_result = es_client.count(index="ingestr_test_cloud_index")
        assert count_result["count"] == 3

        # Get all documents
        search_result = es_client.search(
            index="ingestr_test_cloud_index", body={"query": {"match_all": {}}}
        )
        docs = search_result["hits"]["hits"]

        assert len(docs) == 3

        # Verify document content
        names = sorted([doc["_source"]["name"] for doc in docs])
        assert names == ["Alice", "Bob", "Charlie"]

    finally:
        # Clean up
        os.remove(csv_path)
        try:
            # Clean up the test index from cloud
            parsed = urlparse(es_cloud_url.replace("elasticsearch://", "https://"))
            username = parsed.username
            password = parsed.password
            host = parsed.hostname
            port = parsed.port if parsed.port else 443

            es_url = f"https://{host}:{port}"
            es_client = Elasticsearch([es_url], basic_auth=(username, password))
            if es_client.indices.exists(index="ingestr_test_cloud_index"):
                es_client.indices.delete(index="ingestr_test_cloud_index")
        except Exception:
            pass
        try:
            shutil.rmtree(get_abs_path("../pipeline_data"))
        except Exception:
            pass


@pytest.mark.skipif(
    not all(
        [
            os.getenv("SNAPCHAT_REFRESH_TOKEN"),
            os.getenv("SNAPCHAT_CLIENT_ID"),
            os.getenv("SNAPCHAT_CLIENT_SECRET"),
            os.getenv("SNAPCHAT_ORGANIZATION_ID"),
        ]
    ),
    reason="Snapchat credentials not set in environment",
)
def test_snapchat_ads_merge_strategy():
    """Test that Snapchat Ads merge strategy correctly appends data with different breakdowns.

    This test verifies:
    1. First ingest without breakdown - adsquad_id and ad_id should be NULL
    2. Second ingest with ad breakdown - ad_id should be populated
    3. Both sets of records should exist in the table (append, not replace)
    """
    try:
        shutil.rmtree(get_abs_path("../pipeline_data"))
    except Exception:
        pass

    # Get Snapchat credentials from environment
    refresh_token = os.getenv("SNAPCHAT_REFRESH_TOKEN")
    client_id = os.getenv("SNAPCHAT_CLIENT_ID")
    client_secret = os.getenv("SNAPCHAT_CLIENT_SECRET")
    organization_id = os.getenv("SNAPCHAT_ORGANIZATION_ID")

    # Build source URI
    source_uri = (
        f"snapchatads://?refresh_token={refresh_token}"
        f"&client_id={client_id}"
        f"&client_secret={client_secret}"
        f"&organization_id={organization_id}"
    )

    # Create DuckDB database
    db_path = get_abs_path("../test_snapchat_merge.duckdb")
    dest_uri = f"duckdb:///{db_path}"

    try:
        # First ingest: campaigns_stats without breakdown
        # Expected: adsquad_id and ad_id should be NULL
        result1 = invoke_ingest_command(
            source_uri,
            "campaigns_stats:HOUR,impressions,spend",
            dest_uri,
            "snapchat_ads.campaigns_stats",
            interval_start="2025-11-19",
            interval_end="2025-11-20",
        )

        assert result1.exit_code == 0, f"First ingest failed: {result1.stdout}"

        # Check first ingest results
        conn = duckdb.connect(db_path)

        # First, check what columns exist
        columns = conn.execute(
            "SELECT column_name FROM information_schema.columns "
            "WHERE table_schema = 'snapchat_ads' AND table_name = 'campaigns_stats' "
            "ORDER BY ordinal_position"
        ).fetchall()
        column_names = [col[0] for col in columns]
        print(f"\n✓ Columns after first ingest: {column_names}")

        # Get total count
        first_ingest_total = conn.execute(
            "SELECT COUNT(*) FROM snapchat_ads.campaigns_stats"
        ).fetchone()[0]

        assert first_ingest_total > 0, "First ingest should have data"

        # Check if ad_id and adsquad_id columns exist
        has_ad_id = "ad_id" in column_names
        has_adsquad_id = "adsquad_id" in column_names

        if has_ad_id and has_adsquad_id:
            # Columns exist, check for NULL values
            result = conn.execute(
                "SELECT COUNT(CASE WHEN ad_id IS NULL THEN 1 END) as null_ad_ids, "
                "COUNT(CASE WHEN adsquad_id IS NULL THEN 1 END) as null_adsquad_ids "
                "FROM snapchat_ads.campaigns_stats"
            ).fetchone()
            first_ingest_null_ad_ids = result[0]
            print(
                f"✓ First ingest: {first_ingest_total} records with NULL ad_id and adsquad_id"
            )
            assert first_ingest_null_ad_ids == first_ingest_total, (
                f"All ad_id values should be NULL in first ingest, got {first_ingest_null_ad_ids}/{first_ingest_total}"
            )
        else:
            print(
                f"⚠ First ingest: {first_ingest_total} records but missing columns - "
                f"ad_id: {has_ad_id}, adsquad_id: {has_adsquad_id}"
            )

        # Second ingest: campaigns_stats with ad breakdown
        # Expected: ad_id should be populated, data should be appended
        result2 = invoke_ingest_command(
            source_uri,
            "campaigns_stats:ad,HOUR,impressions,spend",
            dest_uri,
            "snapchat_ads.campaigns_stats",
            interval_start="2025-11-19",
            interval_end="2025-11-20",
        )

        assert result2.exit_code == 0, f"Second ingest failed: {result2.stdout}"

        # Check merge results
        result = conn.execute(
            "SELECT COUNT(*) as total, "
            "COUNT(CASE WHEN ad_id IS NULL THEN 1 END) as null_ad_ids, "
            "COUNT(CASE WHEN ad_id IS NOT NULL THEN 1 END) as non_null_ad_ids "
            "FROM snapchat_ads.campaigns_stats"
        ).fetchone()
        total_records = result[0]
        null_ad_ids = result[1]
        non_null_ad_ids = result[2]

        print(
            f"✓ After second ingest: {total_records} total records "
            f"({null_ad_ids} with NULL ad_id, {non_null_ad_ids} with populated ad_id)"
        )

        # Verify merge strategy worked correctly
        assert total_records > first_ingest_total, (
            f"Second ingest should have appended data, not replaced. Got {total_records} total vs {first_ingest_total} first"
        )

        # If ad_id column existed in first ingest, verify NULL records remain
        if has_ad_id:
            assert null_ad_ids == first_ingest_total, (
                f"NULL ad_id records should remain from first ingest. Got {null_ad_ids} NULL vs {first_ingest_total} first ingest"
            )

        assert non_null_ad_ids > 0, (
            "Should have records with populated ad_id from second ingest"
        )

        # Verify primary key structure - get all columns first
        all_columns = conn.execute(
            "SELECT column_name FROM information_schema.columns "
            "WHERE table_schema = 'snapchat_ads' AND table_name = 'campaigns_stats' "
            "ORDER BY ordinal_position"
        ).fetchall()
        all_column_names = [col[0] for col in all_columns]

        # Select only columns that exist
        select_cols = []
        for col in ["campaign_id", "adsquad_id", "ad_id", "start_time", "end_time"]:
            if col in all_column_names:
                select_cols.append(col)

        result = conn.execute(
            f"SELECT {', '.join(select_cols)} FROM snapchat_ads.campaigns_stats LIMIT 5"
        ).fetchall()

        print("✓ Sample records (showing PK columns):")
        for row in result:
            row_str = ", ".join(f"{col}={val}" for col, val in zip(select_cols, row))
            print(f"  {row_str}")

        conn.close()

        print("Merge strategy test passed!")
        print(
            f"   - First ingest created {first_ingest_total} records with NULL breakdown IDs"
        )
        print(
            f"   - Second ingest appended {non_null_ad_ids} records with ad breakdown"
        )
        print(f"   - Total: {total_records} records in table")

    finally:
        # Clean up
        if os.path.exists(db_path):
            os.remove(db_path)
        try:
            shutil.rmtree(get_abs_path("../pipeline_data"))
        except Exception:
            pass

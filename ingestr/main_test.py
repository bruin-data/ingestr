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
from dlt.sources.filesystem import glob_files
from fsspec.implementations.memory import MemoryFileSystem  # type: ignore
from sqlalchemy.pool import NullPool
from testcontainers.clickhouse import ClickHouseContainer  # type: ignore
from testcontainers.core.waiting_utils import wait_for_logs  # type: ignore
from testcontainers.kafka import KafkaContainer  # type: ignore
from testcontainers.localstack import LocalStackContainer  # type: ignore
from testcontainers.mongodb import MongoDbContainer  # type: ignore
from testcontainers.mssql import SqlServerContainer  # type: ignore
from testcontainers.mysql import MySqlContainer  # type: ignore
from testcontainers.postgres import PostgresContainer  # type: ignore
from typer.testing import CliRunner

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

    conn = duckdb.connect(abs_db_path)

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
        conn.execute("CHECKPOINT")
        return conn.sql(
            "select symbol, date, is_enabled, name from testschema_merge.output order by symbol asc"
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

    run("csv://ingestr/testdata/merge_part1.csv")
    assert_output_equals_to_csv("./testdata/merge_part1.csv")

    first_run_id = conn.sql(
        "select _dlt_load_id from testschema_merge.output limit 1"
    ).fetchall()[0][0]

    ##############################
    # we'll run again, we don't expect any changes since the data hasn't changed
    run("csv://ingestr/testdata/merge_part1.csv")
    assert_output_equals_to_csv("./testdata/merge_part1.csv")

    # we also ensure that the other rows were not touched
    count_by_run_id = conn.sql(
        "select _dlt_load_id, count(*) from testschema_merge.output group by 1"
    ).fetchall()
    assert len(count_by_run_id) == 1
    assert count_by_run_id[0][1] == 3
    assert count_by_run_id[0][0] == first_run_id
    ##############################

    ##############################
    # now we'll run the same ingestion but with a different file this time

    run("csv://ingestr/testdata/merge_part2.csv")
    assert_output_equals_to_csv("./testdata/merge_expected.csv")

    # let's check the runs
    count_by_run_id = conn.sql(
        "select _dlt_load_id, count(*) from testschema_merge.output group by 1 order by 1 asc"
    ).fetchall()

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


class EphemeralDuckDb:
    def start(self) -> str:
        abs_path = get_abs_path(f"./testdata/duckdb_{get_random_string(5)}.db")
        return f"duckdb:///{abs_path}"

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


POSTGRES_IMAGE = "postgres:16.3-alpine3.20"
MYSQL8_IMAGE = "mysql:8.4.1"
MSSQL22_IMAGE = "mcr.microsoft.com/mssql/server:2022-CU13-ubuntu-22.04"
CLICKHOUSE_IMAGE = "clickhouse/clickhouse-server:24.12"

pgDocker = DockerImage(
    "postgres", lambda: PostgresContainer(POSTGRES_IMAGE, driver=None).start()
)
clickHouseDocker = ClickhouseDockerImage(
    "clickhouse", lambda: ClickHouseContainer(CLICKHOUSE_IMAGE).start()
)
mysqlDocker = DockerImage(
    "mysql", lambda: MySqlContainer(MYSQL8_IMAGE, username="root").start()
)

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
def manage_containers(request):
    shared_dir = request.config.workerinput["shared_directory"]
    unique_containers = set(SOURCES.values()) | set(DESTINATIONS.values())
    for container in unique_containers:
        container.container_lock_dir = shared_dir


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


@pytest.mark.parametrize(
    "dest", list(DESTINATIONS.values()), ids=list(DESTINATIONS.keys())
)
@pytest.mark.parametrize("source", list(SOURCES.values()), ids=list(SOURCES.keys()))
def test_create_replace(source, dest):
    with ThreadPoolExecutor() as executor:
        source_future = executor.submit(source.start)
        dest_future = executor.submit(dest.start)
        source_uri = source_future.result()
        dest_uri = dest_future.result()
    db_to_db_create_replace(source_uri, dest_uri)
    source.stop()
    dest.stop()


@pytest.mark.parametrize(
    "dest", list(DESTINATIONS.values()), ids=list(DESTINATIONS.keys())
)
@pytest.mark.parametrize("source", list(SOURCES.values()), ids=list(SOURCES.keys()))
def test_append(source, dest):
    with ThreadPoolExecutor() as executor:
        source_future = executor.submit(source.start)
        dest_future = executor.submit(dest.start)
        source_uri = source_future.result()
        dest_uri = dest_future.result()
    db_to_db_append(source_uri, dest_uri)
    source.stop()
    dest.stop()


@pytest.mark.parametrize(
    "dest", list(DESTINATIONS.values()), ids=list(DESTINATIONS.keys())
)
@pytest.mark.parametrize("source", list(SOURCES.values()), ids=list(SOURCES.keys()))
def test_merge_with_primary_key(source, dest):
    with ThreadPoolExecutor() as executor:
        source_future = executor.submit(source.start)
        dest_future = executor.submit(dest.start)
        source_uri = source_future.result()
        dest_uri = dest_future.result()
    db_to_db_merge_with_primary_key(source_uri, dest_uri)
    source.stop()
    dest.stop()


@pytest.mark.parametrize(
    "dest", list(DESTINATIONS.values()), ids=list(DESTINATIONS.keys())
)
@pytest.mark.parametrize("source", list(SOURCES.values()), ids=list(SOURCES.keys()))
def test_delete_insert_without_primary_key(source, dest):
    with ThreadPoolExecutor() as executor:
        source_future = executor.submit(source.start)
        dest_future = executor.submit(dest.start)
        source_uri = source_future.result()
        dest_uri = dest_future.result()
    db_to_db_delete_insert_without_primary_key(source_uri, dest_uri)
    source.stop()
    dest.stop()


@pytest.mark.parametrize(
    "dest", list(DESTINATIONS.values()), ids=list(DESTINATIONS.keys())
)
@pytest.mark.parametrize("source", list(SOURCES.values()), ids=list(SOURCES.keys()))
def test_delete_insert_with_time_range(source, dest):
    with ThreadPoolExecutor() as executor:
        source_future = executor.submit(source.start)
        dest_future = executor.submit(dest.start)
        source_uri = source_future.result()
        dest_uri = dest_future.result()
    db_to_db_delete_insert_with_timerange(source_uri, dest_uri)
    source.stop()
    dest.stop()


def db_to_db_create_replace(source_connection_url: str, dest_connection_url: str):
    schema_rand_prefix = f"testschema_create_replace_{get_random_string(5)}"
    try:
        shutil.rmtree(get_abs_path("../pipeline_data"))
    except Exception:
        pass

    source_engine = sqlalchemy.create_engine(source_connection_url)
    with source_engine.begin() as conn:
        conn.execute(f"DROP SCHEMA IF EXISTS {schema_rand_prefix}")
        conn.execute(f"CREATE SCHEMA {schema_rand_prefix}")
        conn.execute(
            f"CREATE TABLE {schema_rand_prefix}.input (id INTEGER, val VARCHAR(20), updated_at DATE)"
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

    result = invoke_ingest_command(
        source_connection_url,
        f"{schema_rand_prefix}.input",
        dest_connection_url,
        f"{schema_rand_prefix}.output",
    )

    assert result.exit_code == 0

    dest_engine = sqlalchemy.create_engine(dest_connection_url)
    res = dest_engine.execute(
        f"select id, val, updated_at from {schema_rand_prefix}.output"
    ).fetchall()

    assert len(res) == 2
    assert res[0] == (1, "val1", as_datetime("2022-01-01"))
    assert res[1] == (2, "val2", as_datetime("2022-02-01"))


def db_to_db_append(source_connection_url: str, dest_connection_url: str):
    schema_rand_prefix = f"testschema_append_{get_random_string(5)}"
    try:
        shutil.rmtree(get_abs_path("../pipeline_data"))
    except Exception:
        pass

    source_engine = sqlalchemy.create_engine(source_connection_url)
    with source_engine.begin() as conn:
        conn.execute(f"DROP SCHEMA IF EXISTS {schema_rand_prefix}")
        conn.execute(f"CREATE SCHEMA {schema_rand_prefix}")
        conn.execute(
            f"CREATE TABLE {schema_rand_prefix}.input (id INTEGER, val VARCHAR(20), updated_at DATE)"
        )
        conn.execute(
            f"INSERT INTO {schema_rand_prefix}.input VALUES (1, 'val1', '2022-01-01'), (2, 'val2', '2022-01-02')"
        )
        res = conn.execute(
            f"select count(*) from {schema_rand_prefix}.input"
        ).fetchall()
        assert res[0][0] == 2

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

    dest_engine = sqlalchemy.create_engine(dest_connection_url)

    def get_output_table():
        return dest_engine.execute(
            f"select id, val, updated_at from {schema_rand_prefix}.output order by id asc"
        ).fetchall()

    run()

    res = get_output_table()
    assert len(res) == 2
    assert res[0] == (1, "val1", as_datetime("2022-01-01"))
    assert res[1] == (2, "val2", as_datetime("2022-01-02"))
    dest_engine.dispose()

    # # run again, nothing should be inserted into the output table
    run()

    res = get_output_table()
    assert len(res) == 2
    assert res[0] == (1, "val1", as_datetime("2022-01-01"))
    assert res[1] == (2, "val2", as_datetime("2022-01-02"))


def db_to_db_merge_with_primary_key(
    source_connection_url: str, dest_connection_url: str
):
    schema_rand_prefix = f"testschema_merge_{get_random_string(5)}"
    try:
        shutil.rmtree(get_abs_path("../pipeline_data"))
    except Exception:
        pass

    source_engine = sqlalchemy.create_engine(source_connection_url)
    with source_engine.begin() as conn:
        conn.execute(f"DROP SCHEMA IF EXISTS {schema_rand_prefix}")
        conn.execute(f"CREATE SCHEMA {schema_rand_prefix}")
        conn.execute(
            f"CREATE TABLE {schema_rand_prefix}.input (id INTEGER NOT NULL, val VARCHAR(20), updated_at DATE NOT NULL)"
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

    dest_engine = sqlalchemy.create_engine(dest_connection_url)

    def get_output_rows():
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
    assert_output_equals(
        [(1, "val1", as_datetime("2022-01-01")), (2, "val2", as_datetime("2022-02-01"))]
    )

    first_run_id = dest_engine.execute(
        f"select _dlt_load_id from {schema_rand_prefix}.output limit 1"
    ).fetchall()[0][0]

    dest_engine.dispose()

    ##############################
    # we'll run again, we don't expect any changes since the data hasn't changed
    res = run()
    assert_output_equals(
        [(1, "val1", as_datetime("2022-01-01")), (2, "val2", as_datetime("2022-02-01"))]
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

    run()
    assert_output_equals(
        [(1, "val1", as_datetime("2022-01-01")), (2, "val2", as_datetime("2022-02-01"))]
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

    run()
    assert_output_equals(
        [(1, "val1", as_datetime("2022-01-01")), (2, "val2", as_datetime("2022-02-01"))]
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

    run()
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

    run()
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
    ##############################


def db_to_db_delete_insert_without_primary_key(
    source_connection_url: str, dest_connection_url: str
):
    schema_rand_prefix = f"testschema_delete_insert_{get_random_string(5)}"
    try:
        shutil.rmtree(get_abs_path("../pipeline_data"))
    except Exception:
        pass

    source_engine = sqlalchemy.create_engine(source_connection_url)
    with source_engine.begin() as conn:
        conn.execute(f"DROP SCHEMA IF EXISTS {schema_rand_prefix}")
        conn.execute(f"CREATE SCHEMA {schema_rand_prefix}")
        conn.execute(
            f"CREATE TABLE {schema_rand_prefix}.input (id INTEGER, val VARCHAR(20), updated_at DATE)"
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

    dest_engine = sqlalchemy.create_engine(dest_connection_url)

    def get_output_rows():
        return dest_engine.execute(
            f"select id, val, updated_at from {schema_rand_prefix}.output order by id asc"
        ).fetchall()

    def assert_output_equals(expected):
        res = get_output_rows()
        assert len(res) == len(expected)
        for i, row in enumerate(expected):
            assert res[i] == row

    run()
    assert_output_equals(
        [(1, "val1", as_datetime("2022-01-01")), (2, "val2", as_datetime("2022-02-01"))]
    )

    first_run_id = dest_engine.execute(
        f"select _dlt_load_id from {schema_rand_prefix}.output limit 1"
    ).fetchall()[0][0]
    dest_engine.dispose()

    ##############################
    # we'll run again, since this is a delete+insert, we expect the run ID to change for the last one
    res = run()
    assert_output_equals(
        [(1, "val1", as_datetime("2022-01-01")), (2, "val2", as_datetime("2022-02-01"))]
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

    run()
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
    source_connection_url: str, dest_connection_url: str
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

    dest_engine = sqlalchemy.create_engine(dest_connection_url, poolclass=NullPool)

    def get_output_rows():
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
        dest_engine = sqlalchemy.create_engine(dest_uri)
        with dest_engine.connect() as conn:
            res = conn.execute(
                "select _kafka__data from testschema.output order by _kafka_msg_id asc"
            ).fetchall()
        dest_engine.dispose()
        return res

    run()

    res = get_output_table()
    assert len(res) == 3
    assert res[0] == ("message1",)
    assert res[1] == ("message2",)
    assert res[2] == ("message3",)

    # run again, nothing should be inserted into the output table
    run()

    res = get_output_table()
    assert len(res) == 3
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
    assert res[0] == ("message1",)
    assert res[1] == ("message2",)
    assert res[2] == ("message3",)
    assert res[3] == ("message4",)

    kafka.stop()


@pytest.mark.parametrize(
    "dest", list(DESTINATIONS.values()), ids=list(DESTINATIONS.keys())
)
def test_arrow_mmap_to_db_create_replace(dest):
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

    dest_engine = sqlalchemy.create_engine(dest_uri)
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

    dest_engine = sqlalchemy.create_engine(dest_uri)

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

    # the first load, it should be loaded correctly
    with dest_engine.begin() as conn:
        res = conn.execute(f"select count(*) from {schema}.output").fetchall()
        assert res[0][0] == row_count

        res = conn.execute(
            f"select date, count(*) from {schema}.output group by 1 order by 1 asc"
        ).fetchall()
        assert res[0][0] == build_datetime("2024-11-05")
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
        assert res[0][0] == build_datetime("2024-11-05")
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
        assert res[0][0] == build_datetime("2024-11-05")
        assert res[0][1] == row_count
        assert res[1][0] == build_datetime("2024-11-06")
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
        assert res[0][0] == build_datetime("2024-11-05")
        assert res[0][1] == row_count
        assert res[1][0] == build_datetime("2024-11-06")
        assert res[1][1] == 1000


@pytest.mark.parametrize(
    "dest", list(DESTINATIONS.values()), ids=list(DESTINATIONS.keys())
)
def test_arrow_mmap_to_db_merge_without_incremental(dest):
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
    dest_engine = sqlalchemy.create_engine(dest_uri)

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


@pytest.mark.parametrize(
    "dest", list(DESTINATIONS.values()), ids=list(DESTINATIONS.keys())
)
@pytest.mark.parametrize("source", list(SOURCES.values()), ids=list(SOURCES.keys()))
def test_db_to_db_exclude_columns(source, dest):
    with ThreadPoolExecutor() as executor:
        source_future = executor.submit(source.start)
        dest_future = executor.submit(dest.start)
        source_uri = source_future.result()
        dest_uri = dest_future.result()

    schema_rand_prefix = f"testschema_db_to_db_exclude_columns_{get_random_string(5)}"

    source_engine = sqlalchemy.create_engine(source_uri)
    with source_engine.begin() as conn:
        conn.execute(f"DROP SCHEMA IF EXISTS {schema_rand_prefix}")
        conn.execute(f"CREATE SCHEMA {schema_rand_prefix}")
        conn.execute(
            f"CREATE TABLE {schema_rand_prefix}.input (id INTEGER, val VARCHAR(20), updated_at DATE, col_to_exclude1 VARCHAR(20), col_to_exclude2 VARCHAR(20))"
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
    result = invoke_ingest_command(
        source_uri,
        f"{schema_rand_prefix}.input",
        dest_uri,
        f"{schema_rand_prefix}.output",
        sql_exclude_columns="col_to_exclude1,col_to_exclude2",
    )

    assert result.exit_code == 0

    dest_engine = sqlalchemy.create_engine(dest_uri)
    res = dest_engine.execute(
        f"select id, val, updated_at from {schema_rand_prefix}.output"
    ).fetchall()

    assert len(res) == 2
    assert res[0] == (1, "val1", as_datetime("2022-01-01"))
    assert res[1] == (2, "val2", as_datetime("2022-02-01"))

    # Verify excluded columns don't exist in destination schema
    columns = dest_engine.execute(
        f"SELECT column_name FROM information_schema.columns WHERE table_schema = '{schema_rand_prefix}' AND table_name = 'output'"
    ).fetchall()
    assert columns == [("id",), ("val",), ("updated_at",)]
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

    def smoke_test(dest_uri, dynamodb):
        dest_table = f"public.dynamodb_{get_random_string(5)}"

        result = invoke_ingest_command(
            dynamodb.uri, dynamodb.db_name, dest_uri, dest_table, "append", "updated_at"
        )
        assert_success(result)

        result = get_query_result(
            dest_uri, f"select id, updated_at from {dest_table} ORDER BY id"
        )
        assert len(result) == 3
        for i in range(len(result)):
            assert result[i][0] == dynamodb.data[i]["id"]
            assert result[i][1] == pendulum.parse(dynamodb.data[i]["updated_at"])

    def append_test(dest_uri, dynamodb):
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
            result = get_query_result(
                dest_uri, f"select id, updated_at from {dest_table} ORDER BY id"
            )
            assert len(result) == 3
            for i in range(len(result)):
                assert result[i][0] == dynamodb.data[i]["id"]
                assert result[i][1] == pendulum.parse(dynamodb.data[i]["updated_at"])

    def incremental_test_factory(strategy):
        def incremental_test(dest_uri, dynamodb):
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
            rows = get_query_result(
                dest_uri, f"select id, updated_at from {dest_table} ORDER BY id"
            )
            assert len(rows) == 2
            for i in range(len(rows)):
                assert rows[i][0] == dynamodb.data[i]["id"]
                assert rows[i][1] == pendulum.parse(dynamodb.data[i]["updated_at"])

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

                rows = get_query_result(
                    dest_uri, f"select id, updated_at from {dest_table} ORDER BY id"
                )
                rows_expected = 3
                if strategy == "replace":
                    # old rows are removed in replace
                    rows_expected = 2

                assert len(rows) == rows_expected
                for row in rows:
                    id = int(row[0]) - 1
                    assert row[0] == dynamodb.data[id]["id"]
                    assert row[1] == pendulum.parse(dynamodb.data[id]["updated_at"])

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
    testcase(dest.start(), dynamodb)
    dest.stop()


def get_query_result(uri: str, query: str):
    engine = sqlalchemy.create_engine(uri, poolclass=NullPool)
    with engine.connect() as conn:
        res = conn.execute(query).fetchall()

    return res


def custom_query_tests():
    def replace(source_connection_url, dest_connection_url):
        schema = f"testschema_cr_cust_{get_random_string(5)}"
        with sqlalchemy.create_engine(
            source_connection_url, poolclass=NullPool
        ).connect() as conn:
            conn.execute(f"DROP SCHEMA IF EXISTS {schema}")
            conn.execute(f"CREATE SCHEMA {schema}")
            conn.execute(
                f"CREATE TABLE {schema}.orders (id INTEGER, name VARCHAR(255) NOT NULL, updated_at DATE)"
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

        res = get_query_result(
            dest_connection_url,
            f"select id, order_id, subname, updated_at from {schema}.output order by id asc",
        )

        assert len(res) == 4
        assert res[0] == (1, 1, "Item 1 for First Order", as_datetime("2024-01-01"))
        assert res[1] == (2, 1, "Item 2 for First Order", as_datetime("2024-01-01"))
        assert res[2] == (3, 2, "Item 1 for Second Order", as_datetime("2024-01-01"))
        assert res[3] == (4, 3, "Item 1 for Third Order", as_datetime("2024-01-01"))

    def merge(source_connection_url, dest_connection_url):
        schema = f"testschema_merge_cust_{get_random_string(5)}"
        source_engine = sqlalchemy.create_engine(
            source_connection_url, poolclass=NullPool
        )
        with source_engine.begin() as conn:
            conn.execute(f"DROP SCHEMA IF EXISTS {schema}")
            conn.execute(f"CREATE SCHEMA {schema}")
            conn.execute(
                f"CREATE TABLE {schema}.orders (id INTEGER, name VARCHAR(255) NOT NULL, updated_at DATE)"
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

        res = get_query_result(
            dest_connection_url,
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
        res = get_query_result(
            dest_connection_url,
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
        res = get_query_result(
            dest_connection_url,
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
    testcase(source.start(), dest.start())


# Integration testing when the access token is not provided, and it is only for the resource "repo_events
@pytest.mark.parametrize(
    "dest", list(DESTINATIONS.values()), ids=list(DESTINATIONS.keys())
)
def test_github_to_duckdb(dest):
    dest_uri = dest.start()
    source_uri = "github://?owner=bruin-data&repo=ingestr"
    source_table = "repo_events"

    dest_table = "dest.github_repo_events"
    res = invoke_ingest_command(source_uri, source_table, dest_uri, dest_table)

    assert res.exit_code == 0
    dest_engine = sqlalchemy.create_engine(dest_uri, poolclass=NullPool)
    res = dest_engine.execute(f"select count(*) from {dest_table}").fetchall()
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

    def test_no_report_instances_found(dest_uri):
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

    def test_no_ongoing_reports_found(dest_uri):
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

    def test_no_such_report(dest_uri):
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

    def test_successful_ingestion(dest_uri):
        """
        When there are report instances for the given date range, the data should be ingested
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

        dest_engine = sqlalchemy.create_engine(dest_uri)
        count = dest_engine.execute(f"select count(*) from {dest_table}").fetchone()[0]
        assert count == 3

    def test_incremental_ingestion(dest_uri):
        """
        when the pipeline is run till a specific end date, the next ingestion
        should load data from the last processing date, given that last_date is not provided
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

        dest_engine = sqlalchemy.create_engine(dest_uri)
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

        dest_engine = sqlalchemy.create_engine(dest_uri)
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
    test_case(dest.start())
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
            rows = conn.execute(f"select count(*) from {dest_table}").fetchall()
            assert len(rows) == 1
            assert rows[0] == (n,)

    def test_empty_source_uri(dest_uri):
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

    def test_unsupported_file_format(dest_uri):
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

    def test_missing_credentials(dest_uri):
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

    def test_csv_load(dest_uri):
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
            assert_rows(dest_uri, dest_table, 5)

    def test_csv_gz_load(dest_uri):
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
            assert_rows(dest_uri, dest_table, 5)

    def test_parquet_load(dest_uri):
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
            assert_rows(dest_uri, dest_table, 5)

    def test_parquet_gz_load(dest_uri):
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
            assert_rows(dest_uri, dest_table, 5)

    def test_jsonl_load(dest_uri):
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
            assert_rows(dest_uri, dest_table, 5)

    def test_jsonl_gz_load(dest_uri):
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
            assert_rows(dest_uri, dest_table, 5)

    def test_glob_load(dest_uri):
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
            assert_rows(dest_uri, dest_table, 6)

    def test_compound_table_name(dest_uri):
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
            assert_rows(dest_uri, dest_table, 6)

    def test_uri_precedence(dest_uri):
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
            assert_rows(dest_uri, dest_table, 6)

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
    test_case(dest.start())
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
    test_case(dest.start())
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
    def invalid_source_table(dest_uri):
        schema = f"testschema_frankfurter_{get_random_string(5)}"
        dest_table = f"{schema}.frankfurter_{get_random_string(5)}"
        result = invoke_ingest_command(
            "frankfurter://",
            "invalid table",
            dest_uri,
            dest_table,
        )
        assert result.exit_code != 0
        assert has_exception(result.exception, UnsupportedResourceError)

    def interval_start_does_not_exceed_interval_end(dest_uri):
        schema = f"testschema_frankfurter_{get_random_string(5)}"
        dest_table = f"{schema}.frankfurter_{get_random_string(5)}"
        result = invoke_ingest_command(
            "frankfurter://",
            "exchange_rates",
            dest_uri,
            dest_table,
            interval_start="2025-04-11",
            interval_end="2025-04-10",
        )
        assert result.exit_code != 0
        assert has_exception(result.exception, ValueError)
        assert "Interval-end cannot be before interval-start." in str(result.exception)

    def interval_start_can_equal_interval_end(dest_uri):
        schema = f"testschema_frankfurter_{get_random_string(5)}"
        dest_table = f"{schema}.frankfurter_{get_random_string(5)}"
        result = invoke_ingest_command(
            "frankfurter://",
            "exchange_rates",
            dest_uri,
            dest_table,
            interval_start="2025-04-10",
            interval_end="2025-04-10",
        )
        assert result.exit_code == 0

    def interval_start_does_not_exceed_current_date(dest_uri):
        schema = f"testschema_frankfurter_{get_random_string(5)}"
        dest_table = f"{schema}.frankfurter_{get_random_string(5)}"
        start_date = pendulum.now().add(days=1).format("YYYY-MM-DD")
        result = invoke_ingest_command(
            "frankfurter://",
            "exchange_rates",
            dest_uri,
            dest_table,
            interval_start=start_date,
        )
        assert result.exit_code != 0
        assert has_exception(result.exception, ValueError)
        assert "Interval-start cannot be in the future." in str(result.exception)

    def interval_end_does_not_exceed_current_date(dest_uri):
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
        )
        assert result.exit_code != 0
        assert has_exception(result.exception, ValueError)
        assert "Interval-end cannot be in the future." in str(result.exception)

    def exchange_rate_on_specific_date(dest_uri):
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
        )
        assert result.exit_code == 0, f"Ingestion failed: {result.output}"

        dest_engine = sqlalchemy.create_engine(dest_uri)

        query = f"SELECT rate FROM {dest_table} WHERE currency_code = 'GBP'"
        with dest_engine.connect() as conn:
            rows = conn.execute(query).fetchall()

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
    test_case(dest.start())
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

    result = invoke_ingest_command(
        source_uri,
        f"{schema_rand_prefix}.input",
        dest_uri,
        f"{schema_rand_prefix}.output",
        sql_backend="sqlalchemy",
    )

    assert result.exit_code == 0

    dest_engine = sqlalchemy.create_engine(dest_uri)
    res = dest_engine.execute(f"select * from {schema_rand_prefix}.output").fetchall()

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
    assert res[0] == ("Row 1", "1970-01-01 00:00:00")
    assert res[1] == ("Row 2", "2024-01-01 12:00:00")
    assert res[2] == ("Row 3", "1970-01-01 00:00:00")
    assert res[3] == ("Row 4", "2025-04-05 08:30:00")
    assert res[4] == ("Row 5", "1970-01-01 00:00:00")


def appsflyer_test_cases():
    source_uri = "appsflyer://?api_key=" + os.environ.get(
        "INGESTR_TEST_APPSFLYER_TOKEN", ""
    )

    def creatives(dest_uri: str):
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

        with sqlalchemy.create_engine(dest_uri).connect() as conn:
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

    def campaigns(dest_uri: str):
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

        with sqlalchemy.create_engine(dest_uri).connect() as conn:
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

    def custom(dest_uri: str):
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

        with sqlalchemy.create_engine(dest_uri).connect() as conn:
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
    testcase(dest.start())
    dest.stop()


def airtable_test_cases():
    def table_with_base_id(dest_uri: str):
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

        with sqlalchemy.create_engine(dest_uri).connect() as conn:
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
    testcase(dest.start())
    dest.stop()


def pp(x):
    import sys

    print(x, file=sys.stderr)


@pytest.mark.parametrize(
    "dest", list(DESTINATIONS.values()), ids=list(DESTINATIONS.keys())
)
def test_mongodb_source(dest):
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

    try:
        invoke_ingest_command(
            mongo.get_connection_url(),
            "test_db.test_collection",
            dest_uri,
            "raw.test_collection",
        )

        with sqlalchemy.create_engine(dest_uri).connect() as conn:
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

    conn = duckdb.connect(abs_db_path)

    # Run ingest command
    result = invoke_ingest_command(
        f"stripe://{stripe_table}s?api_key={stripe_token}",
        stripe_table,
        uri,
        f"raw.{stripe_table}s",
    )

    assert result.exit_code == 0

    # Verify data was loaded
    res = conn.sql(f"select count(*) from raw.{stripe_table}s").fetchone()
    assert res[0] > 0, f"No {stripe_table} records found"

    # Clean up
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

    conn = duckdb.connect(abs_db_path)

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
    res = conn.sql(f"select count(*) from raw.{stripe_table}s").fetchone()
    assert res[0] > 0, f"No {stripe_table} records found"

    # Clean up
    try:
        os.remove(abs_db_path)
    except Exception:
        pass


def trustpilot_test_case(dest_uri):
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

        engine = sqlalchemy.create_engine(dest_uri)
        with engine.connect() as conn:
            rows = conn.execute(f"SELECT * FROM {dest_table}").fetchall()
            assert len(rows) > 0, "No data ingested into the destination"


@pytest.mark.parametrize(
    "dest", list(DESTINATIONS.values()), ids=list(DESTINATIONS.keys())
)
def test_trustpilot(dest):
    trustpilot_test_case(dest.start())
    dest.stop()

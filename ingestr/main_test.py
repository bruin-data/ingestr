import csv
import os
import random
import shutil
import string
import tempfile
import time
from concurrent.futures import ThreadPoolExecutor
from datetime import date, datetime, timezone
from typing import Optional

import duckdb
import numpy as np
import pandas as pd  # type: ignore
import pyarrow as pa  # type: ignore
import pyarrow.ipc as ipc  # type: ignore
import pytest
import sqlalchemy
from confluent_kafka import Producer  # type: ignore
from testcontainers.kafka import KafkaContainer  # type: ignore
from testcontainers.mssql import SqlServerContainer  # type: ignore
from testcontainers.mysql import MySqlContainer  # type: ignore
from testcontainers.postgres import PostgresContainer  # type: ignore
from testcontainers.localstack import LocalStackContainer
from testcontainers.core.waiting_utils import wait_for_logs
from typer.testing import CliRunner

from ingestr.main import app

runner = CliRunner()


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

    result = runner.invoke(
        app,
        args,
        input="y\n",
        env={"DISABLE_TELEMETRY": "true"},
    )
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
            actual_rows.append(row)

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
    def __init__(self, container_creator, connection_suffix: str = "") -> None:
        self.container_creator = container_creator
        self.connection_suffix = connection_suffix
        self.container = None
        self.starting = False

    def start(self) -> str:
        if self.container:
            return self.container.get_connection_url() + self.connection_suffix

        if self.starting:
            while self.container is None:
                time.sleep(0.1)

            return self.container.get_connection_url() + self.connection_suffix

        self.starting = True
        self.container = self.container_creator()
        self.starting = False
        if self.container:
            return self.container.get_connection_url() + self.connection_suffix

        raise Exception("Failed to start container")

    def stop(self):
        pass

    def stop_fully(self):
        if self.container:
            self.container.stop()


class DuckDb:
    def start(self) -> str:
        self.abs_path = get_abs_path(f"./testdata/duckdb_{get_random_string(5)}.db")
        return f"duckdb:///{self.abs_path}"

    def stop(self):
        try:
            os.remove(self.abs_db_path)
        except Exception:
            pass

    def stop_fully(self):
        self.stop()


POSTGRES_IMAGE = "postgres:16.3-alpine3.20"
MYSQL8_IMAGE = "mysql:8.4.1"
MSSQL22_IMAGE = "mcr.microsoft.com/mssql/server:2022-preview-ubuntu-22.04"

pgDocker = DockerImage(lambda: PostgresContainer(POSTGRES_IMAGE, driver=None).start())
SOURCES = {
    "postgres": pgDocker,
    "duckdb": DuckDb(),
    "mysql8": DockerImage(
        lambda: MySqlContainer(MYSQL8_IMAGE, username="root").start()
    ),
    "sqlserver": DockerImage(
        lambda: SqlServerContainer(MSSQL22_IMAGE, dialect="mssql").start(),
        "?driver=ODBC+Driver+18+for+SQL+Server&TrustServerCertificate=Yes",
    ),
}

DESTINATIONS = {
    "postgres": pgDocker,
    "duckdb": DuckDb(),
}


@pytest.fixture(scope="session", autouse=True)
def manage_containers():
    # Run all tests
    yield

    # Get unique containers since some sources and destinations share containers
    unique_containers = set(SOURCES.values()) | set(DESTINATIONS.values())

    # Stop containers in parallel after tests complete
    with ThreadPoolExecutor() as executor:
        futures = [
            executor.submit(container.stop_fully) for container in unique_containers
        ]
        # Wait for all futures to complete
        for future in futures:
            future.result()


@pytest.mark.parametrize(
    "dest", list(DESTINATIONS.values()), ids=list(DESTINATIONS.keys())
)
@pytest.mark.parametrize("source", list(SOURCES.values()), ids=list(SOURCES.keys()))
def test_create_replace(source, dest):
    # Run source.start() and dest.start() in parallel
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
    # Run source.start() and dest.start() in parallel
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
    # Run source.start() and dest.start() in parallel
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
    # Run source.start() and dest.start() in parallel
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
    # Run source.start() and dest.start() in parallel
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
    try:
        shutil.rmtree(get_abs_path("../pipeline_data"))
    except Exception:
        pass

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

    dest_engine = sqlalchemy.create_engine(dest_connection_url)

    def get_output_rows():
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
        [(1, "val1", as_datetime("2022-01-01")), (2, "val2", as_datetime("2022-01-01"))]
    )

    first_run_id = dest_engine.execute(
        f"select _dlt_load_id from {schema_rand_prefix}.output limit 1"
    ).fetchall()[0][0]
    dest_engine.dispose()

    ##############################
    # we'll run again, since this is a delete+insert, we expect the run ID to change for the last one
    res = run("2022-01-01", "2022-01-02")

    assert_output_equals(
        [(1, "val1", as_datetime("2022-01-01")), (2, "val2", as_datetime("2022-01-01"))]
    )

    # both rows should have a new run ID
    count_by_run_id = dest_engine.execute(
        f"select _dlt_load_id, count(*) from {schema_rand_prefix}.output group by 1 order by 1 asc"
    ).fetchall()
    assert len(count_by_run_id) == 1
    assert count_by_run_id[0][0] != first_run_id
    assert count_by_run_id[0][1] == 2
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
        ]
    )

    # there should be 4 rows with 2 distinct run IDs
    count_by_run_id = dest_engine.execute(
        f"select _dlt_load_id, count(*) from {schema_rand_prefix}.output group by 1 order by 1 asc"
    ).fetchall()
    assert len(count_by_run_id) == 2
    assert count_by_run_id[0][1] == 2
    assert count_by_run_id[1][1] == 2
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
    assert len(count_by_run_id) == 3
    assert count_by_run_id[0][1] == 2
    assert count_by_run_id[1][1] == 2
    assert count_by_run_id[2][1] == 2
    dest_engine.dispose()
    ##############################


def as_datetime(date_str: str) -> date:
    return datetime.strptime(date_str, "%Y-%m-%d").replace(tzinfo=timezone.utc).date()


def as_datetime2(date_str: str) -> date:
    return datetime.strptime(date_str, "%Y-%m-%d")


@pytest.mark.parametrize(
    "dest", list(DESTINATIONS.values()), ids=list(DESTINATIONS.keys())
)
def test_kafka_to_db(dest):
    # Run source.start() and dest.start() in parallel
    with ThreadPoolExecutor() as executor:
        dest_future = executor.submit(dest.start)
        source_future = executor.submit(
            KafkaContainer("confluentinc/cp-kafka:7.6.0").start, timeout=60
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

    dest_engine = sqlalchemy.create_engine(dest_uri)

    def get_output_table():
        return dest_engine.execute(
            "select _kafka__data from testschema.output order by _kafka_msg_id asc"
        ).fetchall()

    run()

    res = get_output_table()
    assert len(res) == 3
    assert res[0] == ("message1",)
    assert res[1] == ("message2",)
    assert res[2] == ("message3",)
    dest_engine.dispose()

    # run again, nothing should be inserted into the output table
    run()

    res = get_output_table()
    assert len(res) == 3
    assert res[0] == ("message1",)
    assert res[1] == ("message2",)
    assert res[2] == ("message3",)
    dest_engine.dispose()

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
    dest_engine.dispose()

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

    # the first load, it should be loaded correctly
    with dest_engine.begin() as conn:
        res = conn.execute(f"select count(*) from {schema}.output").fetchall()
        assert res[0][0] == row_count

        res = conn.execute(
            f"select date, count(*) from {schema}.output group by 1 order by 1 asc"
        ).fetchall()
        assert res[0][0] == as_datetime2("2024-11-05")
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
        assert res[0][0] == as_datetime2("2024-11-05")
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
        assert res[0][0] == as_datetime2("2024-11-05")
        assert res[0][1] == row_count
        assert res[1][0] == as_datetime2("2024-11-06")
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
        assert res[0][0] == as_datetime2("2024-11-05")
        assert res[0][1] == row_count
        assert res[1][0] == as_datetime2("2024-11-06")
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
    # Run source.start() and dest.start() in parallel
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

def test_dynamodb_to_duckdb():
    db_name = f"dynamodb_test_{get_random_string(5)}"

    def load_test_data(ls):
        client = ls.get_client("dynamodb")
        table_cfg = { 
            "TableName": db_name, 
            "KeySchema": [
                {
                    "AttributeName": "id",
                    "KeyType": "HASH",
                }
            ],
            "AttributeDefinitions": [
                {
                    "AttributeName": "id",
                    "AttributeType": "S"
                },
            ],
            "ProvisionedThroughput": {
                "ReadCapacityUnits": 35000,
                "WriteCapacityUnits": 35000,
            }
        }
        client.create_table(**table_cfg)
        items = [
            {
                "id": {"S": "1"},
                "updated_at": {"S": "2024-01-01T00:00:00"}
            },
            {
                "id": {"S": "2"},
                "updated_at": {"S": "2024-02-01T00:00:00"}
            },
            {
                "id": {"S": "3"},
                "updated_at": {"S": "2024-02-01T00:00:00"}
            }
        ]
        for item in items:
            client.put_item(TableName=db_name, Item=item)

    local_stack = (
        LocalStackContainer(image="localstack/localstack:4.0.3")
        .with_services("dynamodb")
    )
    local_stack.start()
    wait_for_logs(local_stack, "Ready.")
    load_test_data(local_stack)

    import pdb; pdb.set_trace()
    local_stack.stop()
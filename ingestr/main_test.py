import os
import shutil

import duckdb
import pytest
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

    result = runner.invoke(
        app,
        args,
        input="y\n",
        env={"DISABLE_TELEMETRY": "true"},
    )
    return result


def test_create_replace():
    abs_db_path = get_abs_path("./testdata/test_create_replace.db")
    rel_db_path_to_command = "ingestr/testdata/test_create_replace.db"

    conn = duckdb.connect(abs_db_path)
    conn.execute("DROP SCHEMA IF EXISTS testschema CASCADE")
    conn.execute("CREATE SCHEMA testschema")
    conn.execute(
        "CREATE TABLE testschema.input (id INTEGER, val VARCHAR, updated_at TIMESTAMP)"
    )
    conn.execute("INSERT INTO testschema.input VALUES (1, 'val1', '2022-01-01')")
    conn.execute("INSERT INTO testschema.input VALUES (2, 'val2', '2022-02-01')")

    res = conn.sql("select count(*) from testschema.input").fetchall()
    assert res[0][0] == 2

    result = invoke_ingest_command(
        f"duckdb:///{rel_db_path_to_command}",
        "testschema.input",
        f"duckdb:///{rel_db_path_to_command}",
        "testschema.output",
    )

    assert result.exit_code == 0

    res = conn.sql(
        "select id, val, strftime(updated_at, '%Y-%m-%d') as updated_at from testschema.output"
    ).fetchall()
    assert len(res) == 2
    assert res[0] == (1, "val1", "2022-01-01")
    assert res[1] == (2, "val2", "2022-02-01")


@pytest.mark.skip(
    reason="this doesn't work at the moment due to a bug with dlt: https://github.com/dlt-hub/dlt/issues/971"
)
def test_append():
    try:
        shutil.rmtree(get_abs_path("../pipeline_data"))
    except Exception:
        pass

    abs_db_path = get_abs_path("./testdata/test_append.db")
    rel_db_path_to_command = "ingestr/testdata/test_append.db"
    uri = f"duckdb:///{rel_db_path_to_command}"

    conn = duckdb.connect(abs_db_path)
    conn.execute("DROP SCHEMA IF EXISTS testschema_append CASCADE")
    conn.execute("CHECKPOINT")

    conn.execute("CREATE SCHEMA testschema_append")
    conn.execute(
        "CREATE TABLE testschema_append.input (id INTEGER, val VARCHAR, updated_at DATE)"
    )
    conn.execute(
        "INSERT INTO testschema_append.input VALUES (1, 'val1', '2022-01-01'), (2, 'val2', '2022-01-02')"
    )
    conn.execute("CHECKPOINT")

    res = conn.sql("select count(*) from testschema_append.input").fetchall()
    assert res[0][0] == 2

    def run():
        res = invoke_ingest_command(
            uri,
            "testschema_append.input",
            uri,
            "testschema_append.output",
            "append",
            "updated_at",
        )
        assert res.exit_code == 0

    def get_output_table():
        conn.execute("CHECKPOINT")
        return conn.sql(
            "select id, val, strftime(updated_at, '%Y-%m-%d') as updated_at from testschema_append.output"
        ).fetchall()

    run()

    res = get_output_table()
    assert len(res) == 2
    assert res[0] == (1, "val1", "2022-01-01")
    assert res[1] == (2, "val2", "2022-01-02")

    # # run again, nothing should be inserted into the output table
    run()

    res = get_output_table()
    assert len(res) == 2
    assert res[0] == (1, "val1", "2022-01-01")
    assert res[1] == (2, "val2", "2022-02-01")


def test_merge_with_primary_key():
    try:
        shutil.rmtree(get_abs_path("../pipeline_data"))
    except Exception:
        pass

    abs_db_path = get_abs_path("./testdata/test_merge_with_primary_key.db")
    rel_db_path_to_command = "ingestr/testdata/test_merge_with_primary_key.db"
    uri = f"duckdb:///{rel_db_path_to_command}"

    conn = duckdb.connect(abs_db_path)
    conn.execute("DROP SCHEMA IF EXISTS testschema_merge CASCADE")
    conn.execute("CREATE SCHEMA testschema_merge")
    conn.execute(
        "CREATE TABLE testschema_merge.input (id INTEGER, val VARCHAR, updated_at TIMESTAMP WITH TIME ZONE)"
    )
    conn.execute("INSERT INTO testschema_merge.input VALUES (1, 'val1', '2022-01-01')")
    conn.execute("INSERT INTO testschema_merge.input VALUES (2, 'val2', '2022-02-01')")

    res = conn.sql("select count(*) from testschema_merge.input").fetchall()
    assert res[0][0] == 2

    def run():
        res = invoke_ingest_command(
            uri,
            "testschema_merge.input",
            uri,
            "testschema_merge.output",
            "merge",
            "updated_at",
            "id",
        )
        assert res.exit_code == 0
        return res

    def get_output_rows():
        conn.execute("CHECKPOINT")
        return conn.sql(
            "select id, val, strftime(updated_at, '%Y-%m-%d') as updated_at from testschema_merge.output order by id asc"
        ).fetchall()

    def assert_output_equals(expected):
        res = get_output_rows()
        assert len(res) == len(expected)
        for i, row in enumerate(expected):
            assert res[i] == row

    run()
    assert_output_equals([(1, "val1", "2022-01-01"), (2, "val2", "2022-02-01")])

    first_run_id = conn.sql(
        "select _dlt_load_id from testschema_merge.output limit 1"
    ).fetchall()[0][0]

    ##############################
    # we'll run again, we don't expect any changes since the data hasn't changed
    run()
    assert_output_equals([(1, "val1", "2022-01-01"), (2, "val2", "2022-02-01")])

    # we also ensure that the other rows were not touched
    count_by_run_id = conn.sql(
        "select _dlt_load_id, count(*) from testschema_merge.output group by 1"
    ).fetchall()
    assert len(count_by_run_id) == 1
    assert count_by_run_id[0][1] == 2
    assert count_by_run_id[0][0] == first_run_id
    ##############################

    ##############################
    # now we'll modify the source data but not the updated at, the output table should not be updated
    conn.execute("UPDATE testschema_merge.input SET val = 'val1_modified' WHERE id = 2")

    run()
    assert_output_equals([(1, "val1", "2022-01-01"), (2, "val2", "2022-02-01")])

    # we also ensure that the other rows were not touched
    count_by_run_id = conn.sql(
        "select _dlt_load_id, count(*) from testschema_merge.output group by 1"
    ).fetchall()
    assert len(count_by_run_id) == 1
    assert count_by_run_id[0][1] == 2
    assert count_by_run_id[0][0] == first_run_id
    ##############################

    ##############################
    # now we'll insert a new row but with an old date, the new row will not show up
    conn.execute("INSERT INTO testschema_merge.input VALUES (3, 'val3', '2022-01-01')")

    run()
    assert_output_equals([(1, "val1", "2022-01-01"), (2, "val2", "2022-02-01")])

    # we also ensure that the other rows were not touched
    count_by_run_id = conn.sql(
        "select _dlt_load_id, count(*) from testschema_merge.output group by 1"
    ).fetchall()
    assert len(count_by_run_id) == 1
    assert count_by_run_id[0][1] == 2
    assert count_by_run_id[0][0] == first_run_id
    ##############################

    ##############################
    # now we'll insert a new row but with a new date, the new row will show up
    conn.execute("INSERT INTO testschema_merge.input VALUES (3, 'val3', '2022-02-02')")

    run()
    assert_output_equals(
        [
            (1, "val1", "2022-01-01"),
            (2, "val2", "2022-02-01"),
            (3, "val3", "2022-02-02"),
        ]
    )

    # we have a new run that inserted rows to this table, so the run count should be 2
    count_by_run_id = conn.sql(
        "select _dlt_load_id, count(*) from testschema_merge.output group by 1 order by 2 desc"
    ).fetchall()
    assert len(count_by_run_id) == 2
    assert count_by_run_id[0][1] == 2
    assert count_by_run_id[0][0] == first_run_id
    # we don't care about the run ID
    assert count_by_run_id[1][1] == 1
    ##############################

    ##############################
    # lastly, let's try modifying the updated_at of an old column, it should be updated in the output table
    conn.execute(
        "UPDATE testschema_merge.input SET val='val2_modified', updated_at = '2022-02-03' WHERE id = 2"
    )

    run()
    assert_output_equals(
        [
            (1, "val1", "2022-01-01"),
            (2, "val2_modified", "2022-02-03"),
            (3, "val3", "2022-02-02"),
        ]
    )

    # we have a new run that inserted rows to this table, so the run count should be 2
    count_by_run_id = conn.sql(
        "select _dlt_load_id, count(*) from testschema_merge.output group by 1 order by 2 desc, 1 asc"
    ).fetchall()
    assert len(count_by_run_id) == 3
    assert count_by_run_id[0][1] == 1
    assert count_by_run_id[0][0] == first_run_id
    # we don't care about the rest of the run IDs
    assert count_by_run_id[1][1] == 1
    assert count_by_run_id[2][1] == 1
    ##############################


def test_delete_insert_without_primary_key():
    try:
        shutil.rmtree(get_abs_path("../pipeline_data"))
    except Exception:
        pass

    abs_db_path = get_abs_path("./testdata/test_delete_insert_without_primary_key.db")
    rel_db_path_to_command = (
        "ingestr/testdata/test_delete_insert_without_primary_key.db"
    )
    uri = f"duckdb:///{rel_db_path_to_command}"

    conn = duckdb.connect(abs_db_path)
    conn.execute("DROP SCHEMA IF EXISTS testschema_delete_insert CASCADE")
    conn.execute("CREATE SCHEMA testschema_delete_insert")
    conn.execute(
        "CREATE TABLE testschema_delete_insert.input (id INTEGER, val VARCHAR, updated_at TIMESTAMP WITH TIME ZONE)"
    )
    conn.execute(
        "INSERT INTO testschema_delete_insert.input VALUES (1, 'val1', '2022-01-01')"
    )
    conn.execute(
        "INSERT INTO testschema_delete_insert.input VALUES (2, 'val2', '2022-02-01')"
    )

    res = conn.sql("select count(*) from testschema_delete_insert.input").fetchall()
    assert res[0][0] == 2

    def run():
        res = invoke_ingest_command(
            uri,
            "testschema_delete_insert.input",
            uri,
            "testschema_delete_insert.output",
            inc_strategy="delete+insert",
            inc_key="updated_at",
        )
        assert res.exit_code == 0
        return res

    def get_output_rows():
        conn.execute("CHECKPOINT")
        return conn.sql(
            "select id, val, strftime(updated_at, '%Y-%m-%d') as updated_at from testschema_delete_insert.output order by id asc"
        ).fetchall()

    def assert_output_equals(expected):
        res = get_output_rows()
        assert len(res) == len(expected)
        for i, row in enumerate(expected):
            assert res[i] == row

    run()
    assert_output_equals([(1, "val1", "2022-01-01"), (2, "val2", "2022-02-01")])

    first_run_id = conn.sql(
        "select _dlt_load_id from testschema_delete_insert.output limit 1"
    ).fetchall()[0][0]

    ##############################
    # we'll run again, since this is a delete+insert, we expect the run ID to change for the last one
    run()
    assert_output_equals([(1, "val1", "2022-01-01"), (2, "val2", "2022-02-01")])

    # we ensure that one of the rows is updated with a new run
    count_by_run_id = conn.sql(
        "select _dlt_load_id, count(*) from testschema_delete_insert.output group by 1 order by 1 asc"
    ).fetchall()
    assert len(count_by_run_id) == 2
    assert count_by_run_id[0][0] == first_run_id
    assert count_by_run_id[0][1] == 1
    assert count_by_run_id[1][0] != first_run_id
    assert count_by_run_id[1][1] == 1
    ##############################

    ##############################
    # now we'll insert a few more lines for the same day, the new rows should show up
    conn.execute(
        "INSERT INTO testschema_delete_insert.input VALUES (3, 'val3', '2022-02-01'), (4, 'val4', '2022-02-01')"
    )
    conn.execute("CHECKPOINT")

    run()
    assert_output_equals(
        [
            (1, "val1", "2022-01-01"),
            (2, "val2", "2022-02-01"),
            (3, "val3", "2022-02-01"),
            (4, "val4", "2022-02-01"),
        ]
    )

    # the new rows should have a new run ID, there should be 2 distinct runs now
    count_by_run_id = conn.sql(
        "select _dlt_load_id, count(*) from testschema_delete_insert.output group by 1 order by 2 desc, 1 asc"
    ).fetchall()
    assert len(count_by_run_id) == 2
    assert count_by_run_id[0][0] != first_run_id
    assert count_by_run_id[0][1] == 3  # 2 new rows + 1 old row
    assert count_by_run_id[1][0] == first_run_id
    assert count_by_run_id[1][1] == 1
    ##############################


def test_delete_insert_with_timerange():
    try:
        shutil.rmtree(get_abs_path("../pipeline_data"))
    except Exception:
        pass

    abs_db_path = get_abs_path("./testdata/test_delete_insert_with_timerange.db")
    rel_db_path_to_command = "ingestr/testdata/test_delete_insert_with_timerange.db"
    uri = f"duckdb:///{rel_db_path_to_command}"

    conn = duckdb.connect(abs_db_path)
    conn.execute("DROP SCHEMA IF EXISTS testschema_delete_insert_timerange CASCADE")
    conn.execute("CREATE SCHEMA testschema_delete_insert_timerange")
    conn.execute(
        "CREATE TABLE testschema_delete_insert_timerange.input (id INTEGER, val VARCHAR, updated_at TIMESTAMP WITH TIME ZONE)"
    )
    conn.execute(
        """INSERT INTO testschema_delete_insert_timerange.input VALUES 
            (1, 'val1', '2022-01-01T00:00:00Z'),
            (2, 'val2', '2022-01-01T00:00:00Z'),
            (3, 'val3', '2022-01-02T00:00:00Z'),
            (4, 'val4', '2022-01-02T00:00:00Z'),
            (5, 'val5', '2022-01-03T00:00:00Z'),
            (6, 'val6', '2022-01-03T00:00:00Z')
        """
    )

    res = conn.sql(
        "select count(*) from testschema_delete_insert_timerange.input"
    ).fetchall()
    assert res[0][0] == 6

    def run(start_date: str, end_date: str):
        res = invoke_ingest_command(
            uri,
            "testschema_delete_insert_timerange.input",
            uri,
            "testschema_delete_insert_timerange.output",
            inc_strategy="delete+insert",
            inc_key="updated_at",
            interval_start=start_date,
            interval_end=end_date,
        )
        assert res.exit_code == 0
        return res

    def get_output_rows():
        conn.execute("CHECKPOINT")
        return conn.sql(
            "select id, val, strftime(updated_at, '%Y-%m-%d') as updated_at from testschema_delete_insert_timerange.output order by id asc"
        ).fetchall()

    def assert_output_equals(expected):
        res = get_output_rows()
        assert len(res) == len(expected)
        for i, row in enumerate(expected):
            assert res[i] == row

    run(
        "2022-01-01T00:00:00Z", "2022-01-02T00:00:00Z"
    )  # dlt runs them with the end date exclusive
    assert_output_equals([(1, "val1", "2022-01-01"), (2, "val2", "2022-01-01")])

    first_run_id = conn.sql(
        "select _dlt_load_id from testschema_delete_insert_timerange.output limit 1"
    ).fetchall()[0][0]

    ##############################
    # we'll run again, since this is a delete+insert, we expect the run ID to change for the last one
    run(
        "2022-01-01T00:00:00Z", "2022-01-02T00:00:00Z"
    )  # dlt runs them with the end date exclusive
    assert_output_equals([(1, "val1", "2022-01-01"), (2, "val2", "2022-01-01")])

    # both rows should have a new run ID
    count_by_run_id = conn.sql(
        "select _dlt_load_id, count(*) from testschema_delete_insert_timerange.output group by 1 order by 1 asc"
    ).fetchall()
    assert len(count_by_run_id) == 1
    assert count_by_run_id[0][0] != first_run_id
    assert count_by_run_id[0][1] == 2
    ##############################

    ##############################
    # now run for the day after, new rows should land
    run("2022-01-02T00:00:00Z", "2022-01-03T00:00:00Z")
    assert_output_equals(
        [
            (1, "val1", "2022-01-01"),
            (2, "val2", "2022-01-01"),
            (3, "val3", "2022-01-02"),
            (4, "val4", "2022-01-02"),
        ]
    )

    # there should be 4 rows with 2 distinct run IDs
    count_by_run_id = conn.sql(
        "select _dlt_load_id, count(*) from testschema_delete_insert_timerange.output group by 1 order by 1 asc"
    ).fetchall()
    assert len(count_by_run_id) == 2
    assert count_by_run_id[0][1] == 2
    assert count_by_run_id[1][1] == 2
    ##############################

    ##############################
    # let's bring in the rows for the third day
    run("2022-01-03T00:00:00Z", "2022-01-04T00:00:00Z")
    assert_output_equals(
        [
            (1, "val1", "2022-01-01"),
            (2, "val2", "2022-01-01"),
            (3, "val3", "2022-01-02"),
            (4, "val4", "2022-01-02"),
            (5, "val5", "2022-01-03"),
            (6, "val6", "2022-01-03"),
        ]
    )

    # there should be 6 rows with 3 distinct run IDs
    count_by_run_id = conn.sql(
        "select _dlt_load_id, count(*) from testschema_delete_insert_timerange.output group by 1 order by 1 asc"
    ).fetchall()
    assert len(count_by_run_id) == 3
    assert count_by_run_id[0][1] == 2
    assert count_by_run_id[1][1] == 2
    assert count_by_run_id[2][1] == 2
    ##############################

    ##############################
    # now let's do a backfill for the first day again, the rows should be updated
    conn.execute(
        "UPDATE testschema_delete_insert_timerange.input SET val = 'val1_modified' WHERE id = 1"
    )

    run("2022-01-01T00:00:00Z", "2022-01-02T00:00:00Z")
    assert_output_equals(
        [
            (1, "val1_modified", "2022-01-01"),
            (2, "val2", "2022-01-01"),
            (3, "val3", "2022-01-02"),
            (4, "val4", "2022-01-02"),
            (5, "val5", "2022-01-03"),
            (6, "val6", "2022-01-03"),
        ]
    )

    # there should still be 6 rows with 3 distinct run IDs
    count_by_run_id = conn.sql(
        "select _dlt_load_id, count(*) from testschema_delete_insert_timerange.output group by 1 order by 1 asc"
    ).fetchall()
    assert len(count_by_run_id) == 3
    assert count_by_run_id[0][1] == 2
    assert count_by_run_id[1][1] == 2
    assert count_by_run_id[2][1] == 2
    ##############################

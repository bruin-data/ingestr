import io
import subprocess
import tempfile
import unittest
from dataclasses import dataclass
from datetime import date, datetime
from pathlib import Path
from unittest.mock import patch

import ingestr
import ingestr._data as ingestr_data

try:
    import pyarrow as pa
except ImportError:
    pa = None


class NonClosingBytesIO(io.BytesIO):
    def close(self):
        self.closed_called = True


class FakePopen:
    def __init__(self, args, *, stdin=None, **kwargs):
        self.args = args
        self.kwargs = kwargs
        self.stdin_buffer = NonClosingBytesIO()
        self.stdin = self.stdin_buffer
        self.stdout = io.BytesIO(b"captured stdout") if kwargs.get("stdout") == subprocess.PIPE else None
        self.stderr = io.BytesIO(b"captured stderr") if kwargs.get("stderr") == subprocess.PIPE else None
        self.returncode = 0
        self.killed = False

    def communicate(self):
        return None, None

    def kill(self):
        self.killed = True

    def wait(self):
        return self.returncode

    def poll(self):
        return self.returncode


class IngestrPackageTest(unittest.TestCase):
    def test_build_ingest_args_maps_python_values_to_cli_flags(self):
        args = ingestr.build_ingest_args(
            source_uri="postgres://source",
            dest_uri="duckdb:///tmp/dest.db",
            source_table="public.users",
            dest_table="main.users",
            incremental_predicate="t.event_date >= DATE '2026-01-01'",
            incremental_strategy="merge",
            interval_start=date(2026, 1, 1),
            interval_end=datetime(2026, 1, 2, 3, 4, 5),
            primary_key=["id", "tenant_id"],
            cluster_by=["tenant_id", "id"],
            sql_exclude_columns="internal_note",
            mask=["email:sha256", "phone:redact"],
            trim_whitespace=True,
            full_refresh=True,
            yes=True,
            pipelines_dir=Path("/tmp/pipelines"),
            query_annotations={"asset": "users", "pipeline": "daily"},
            extra_args=["--progress", "log"],
        )

        self.assertEqual(
            args,
            [
                "ingest",
                "--source-uri",
                "postgres://source",
                "--dest-uri",
                "duckdb:///tmp/dest.db",
                "--source-table",
                "public.users",
                "--dest-table",
                "main.users",
                "--incremental-predicate",
                "t.event_date >= DATE '2026-01-01'",
                "--incremental-strategy",
                "merge",
                "--interval-start",
                "2026-01-01",
                "--interval-end",
                "2026-01-02T03:04:05",
                "--primary-key",
                "id",
                "--primary-key",
                "tenant_id",
                "--cluster-by",
                "tenant_id,id",
                "--yes",
                "--full-refresh",
                "--sql-exclude-columns",
                "internal_note",
                "--mask",
                "email:sha256",
                "--mask",
                "phone:redact",
                "--trim-whitespace",
                "--pipelines-dir",
                "/tmp/pipelines",
                "--query-annotations",
                '{"asset":"users","pipeline":"daily"}',
                "--progress",
                "log",
            ],
        )

    def test_run_invokes_bundled_binary(self):
        completed = subprocess.CompletedProcess(["/tmp/ingestr", "--version"], 0)

        with patch("ingestr._runner.binary_path", return_value="/tmp/ingestr"):
            with patch("subprocess.run", return_value=completed) as run:
                result = ingestr.run(["--version"], capture_output=True, text=True)

        self.assertIs(result, completed)
        run.assert_called_once_with(
            ["/tmp/ingestr", "--version"],
            check=True,
            capture_output=True,
            text=True,
        )

    def test_run_cli_runs_generated_args(self):
        completed = subprocess.CompletedProcess(["/tmp/ingestr"], 0)

        with patch("ingestr._runner.run", return_value=completed) as run:
            result = ingestr.run_cli(
                source_uri="csv:///tmp/users.csv",
                dest_uri="sqlite:///tmp/users.db",
                source_table="users",
                dest_table="main.users",
                incremental_predicate="t.event_date >= DATE '2026-01-01'",
                progress="log",
                check=False,
                capture_output=True,
                text=True,
            )

        self.assertIs(result, completed)
        run.assert_called_once_with(
            [
                "ingest",
                "--source-uri",
                "csv:///tmp/users.csv",
                "--dest-uri",
                "sqlite:///tmp/users.db",
                "--source-table",
                "users",
                "--dest-table",
                "main.users",
                "--incremental-predicate",
                "t.event_date >= DATE '2026-01-01'",
                "--progress",
                "log",
            ],
            check=False,
            executable=None,
            capture_output=True,
            text=True,
        )

    def test_run_rejects_shell_command_string(self):
        with self.assertRaises(TypeError):
            ingestr.run("ingest --help")

    def test_binary_path_finds_installed_script(self):
        with tempfile.TemporaryDirectory() as tmp:
            executable = Path(tmp) / "ingestr"
            executable.write_text("#!/bin/sh\n", encoding="utf-8")

            with patch("ingestr._runner._binary_dirs", return_value=[Path(tmp)]):
                with patch("ingestr._runner.shutil.which", return_value=None):
                    self.assertEqual(ingestr.binary_path(), str(executable))

    @unittest.skipIf(pa is None, "pyarrow is required for SDK data ingestion tests")
    def test_ingest_streams_records_to_stdin(self):
        fake = None

        def popen(*args, **kwargs):
            nonlocal fake
            fake = FakePopen(*args, **kwargs)
            return fake

        with patch("ingestr._data.binary_path", return_value="/tmp/ingestr"):
            with patch("subprocess.Popen", side_effect=popen):
                result = ingestr.ingest(
                    [{"id": 1, "name": "Ada"}, {"id": 2, "name": "Grace"}],
                    dest_uri="duckdb:///tmp/out.duckdb",
                    dest_table="main.people",
                    progress="log",
                )

        self.assertEqual(result.returncode, 0)
        self.assertIsNotNone(fake)
        self.assertEqual(fake.args[0], "/tmp/ingestr")
        self.assertIn("--source-uri", fake.args)
        self.assertIn("arrow-stream://-", fake.args)
        self.assertIn("--dest-table", fake.args)
        self.assertIn("main.people", fake.args)

        table = pa.ipc.open_stream(pa.BufferReader(fake.stdin_buffer.getvalue())).read_all()
        self.assertEqual(table.to_pylist(), [{"id": 1, "name": "Ada"}, {"id": 2, "name": "Grace"}])

    @unittest.skipIf(pa is None, "pyarrow is required for SDK data ingestion tests")
    def test_ingest_stream_capture_output_collects_child_pipes(self):
        fake = None

        def popen(*args, **kwargs):
            nonlocal fake
            fake = FakePopen(*args, **kwargs)
            return fake

        with patch("ingestr._data.binary_path", return_value="/tmp/ingestr"):
            with patch("subprocess.Popen", side_effect=popen):
                result = ingestr.ingest(
                    [{"id": 1}],
                    dest_uri="duckdb:///tmp/out.duckdb",
                    dest_table="main.people",
                    capture_output=True,
                )

        self.assertEqual(result.stdout, b"captured stdout")
        self.assertEqual(result.stderr, b"captured stderr")

    @unittest.skipIf(pa is None, "pyarrow is required for SDK data ingestion tests")
    def test_ingest_flattens_pages_into_arrow_stream(self):
        fake = None

        def popen(*args, **kwargs):
            nonlocal fake
            fake = FakePopen(*args, **kwargs)
            return fake

        def fetch_pages():
            yield [{"id": 1}, {"id": 2}]
            yield [{"id": 3}]

        with patch("ingestr._data.binary_path", return_value="/tmp/ingestr"):
            with patch("subprocess.Popen", side_effect=popen):
                ingestr.ingest(
                    fetch_pages(),
                    dest_uri="sqlite:///tmp/out.db",
                    dest_table="main.pages",
                )

        table = pa.ipc.open_stream(pa.BufferReader(fake.stdin_buffer.getvalue())).read_all()
        self.assertEqual(table.to_pylist(), [{"id": 1}, {"id": 2}, {"id": 3}])

    @unittest.skipIf(pa is None, "pyarrow is required for SDK data ingestion tests")
    def test_ingest_accepts_generator_function(self):
        fake = None

        def popen(*args, **kwargs):
            nonlocal fake
            fake = FakePopen(*args, **kwargs)
            return fake

        def fetch_pages():
            yield [{"id": 1}, {"id": 2}]
            yield [{"id": 3}]

        with patch("ingestr._data.binary_path", return_value="/tmp/ingestr"):
            with patch("subprocess.Popen", side_effect=popen):
                ingestr.ingest(
                    fetch_pages,
                    dest_uri="sqlite:///tmp/out.db",
                    dest_table="main.pages",
                )

        table = pa.ipc.open_stream(pa.BufferReader(fake.stdin_buffer.getvalue())).read_all()
        self.assertEqual(table.to_pylist(), [{"id": 1}, {"id": 2}, {"id": 3}])

    @unittest.skipIf(pa is None, "pyarrow is required for SDK data ingestion tests")
    def test_ingest_rejects_async_generator_function(self):
        async def fetch_pages():
            yield [{"id": 1}]

        with self.assertRaisesRegex(TypeError, "async data"):
            ingestr.ingest(
                fetch_pages,
                dest_uri="sqlite:///tmp/out.db",
                dest_table="main.pages",
            )

    @unittest.skipIf(pa is None, "pyarrow is required for SDK data ingestion tests")
    def test_ingest_context_manager_streams_yielded_data(self):
        fake = None

        def popen(*args, **kwargs):
            nonlocal fake
            fake = FakePopen(*args, **kwargs)
            return fake

        def fetch_pages():
            yield [{"id": 2}, {"id": 3}]
            yield pa.table({"id": [4]})

        with patch("ingestr._data.binary_path", return_value="/tmp/ingestr"):
            with patch("subprocess.Popen", side_effect=popen):
                with ingestr.ingest(dest_uri="sqlite:///tmp/out.db", dest_table="main.context") as send:
                    send({"id": 1})
                    for page in fetch_pages():
                        send(page)

                result = send.result

        self.assertIsNotNone(result)
        self.assertEqual(result.returncode, 0)
        table = pa.ipc.open_stream(pa.BufferReader(fake.stdin_buffer.getvalue())).read_all()
        self.assertEqual(table.to_pylist(), [{"id": 1}, {"id": 2}, {"id": 3}, {"id": 4}])

    @unittest.skipIf(pa is None, "pyarrow is required for SDK data ingestion tests")
    def test_ingest_streams_arrow_table(self):
        fake = None

        def popen(*args, **kwargs):
            nonlocal fake
            fake = FakePopen(*args, **kwargs)
            return fake

        table = pa.table({"id": [1, 2], "score": [1.5, 2.5]})

        with patch("ingestr._data.binary_path", return_value="/tmp/ingestr"):
            with patch("subprocess.Popen", side_effect=popen):
                ingestr.ingest(
                    table,
                    dest_uri="sqlite:///tmp/out.db",
                    dest_table="main.df",
                )

        got = pa.ipc.open_stream(pa.BufferReader(fake.stdin_buffer.getvalue())).read_all()
        self.assertEqual(got.to_pylist(), [{"id": 1, "score": 1.5}, {"id": 2, "score": 2.5}])

    @unittest.skipIf(pa is None, "pyarrow is required for SDK data ingestion tests")
    def test_stream_transport_skips_empty_record_batches(self):
        fake = None

        def popen(*args, **kwargs):
            nonlocal fake
            fake = FakePopen(*args, **kwargs)
            return fake

        schema = pa.schema([("id", pa.int64())])
        empty = pa.RecordBatch.from_pylist([], schema=schema)
        non_empty = pa.RecordBatch.from_pylist([{"id": 1}], schema=schema)

        with patch("ingestr._data.binary_path", return_value="/tmp/ingestr"):
            with patch("subprocess.Popen", side_effect=popen):
                ingestr_data._ingest_stream(
                    [empty, non_empty],
                    pa=pa,
                    schema=schema,
                    dest_uri="sqlite:///tmp/out.db",
                    dest_table="main.rows",
                    source_table="python_data",
                )

        batches = list(pa.ipc.open_stream(pa.BufferReader(fake.stdin_buffer.getvalue())))
        self.assertEqual([batch.num_rows for batch in batches], [1])

    @unittest.skipIf(pa is None, "pyarrow is required for SDK data ingestion tests")
    def test_ingest_can_use_mmap_transport(self):
        captured = {}

        def fake_ingest(**kwargs):
            captured.update(kwargs)
            prefix = "mmap://"
            self.assertTrue(kwargs["source_uri"].startswith(prefix))
            path = kwargs["source_uri"][len(prefix):]
            captured["rows"] = pa.ipc.open_file(path).read_all().to_pylist()
            return subprocess.CompletedProcess(["/tmp/ingestr"], 0)

        with tempfile.TemporaryDirectory() as tmp:
            with patch("ingestr._data._cli_ingest", side_effect=fake_ingest):
                result = ingestr.ingest(
                    [{"id": 1}, {"id": 2}],
                    dest_uri="sqlite:///tmp/out.db",
                    dest_table="main.mmap_rows",
                    transport="mmap",
                    temp_dir=tmp,
                )

        self.assertEqual(result.returncode, 0)
        self.assertEqual(captured["source_table"], "python_data")
        self.assertEqual(captured["dest_table"], "main.mmap_rows")
        self.assertEqual(captured["rows"], [{"id": 1}, {"id": 2}])

    @unittest.skipIf(pa is None, "pyarrow is required for SDK data ingestion tests")
    def test_mmap_transport_skips_empty_record_batches(self):
        captured = {}
        schema = pa.schema([("id", pa.int64())])
        empty = pa.RecordBatch.from_pylist([], schema=schema)
        non_empty = pa.RecordBatch.from_pylist([{"id": 1}], schema=schema)

        def fake_ingest(**kwargs):
            captured.update(kwargs)
            prefix = "mmap://"
            self.assertTrue(kwargs["source_uri"].startswith(prefix))
            path = kwargs["source_uri"][len(prefix):]
            reader = pa.ipc.open_file(path)
            captured["batch_lengths"] = [reader.get_batch(i).num_rows for i in range(reader.num_record_batches)]
            return subprocess.CompletedProcess(["/tmp/ingestr"], 0)

        with tempfile.TemporaryDirectory() as tmp:
            with patch("ingestr._data._cli_ingest", side_effect=fake_ingest):
                ingestr_data._ingest_mmap(
                    [empty, non_empty],
                    pa=pa,
                    schema=schema,
                    dest_uri="sqlite:///tmp/out.db",
                    dest_table="main.rows",
                    source_table="python_data",
                    temp_dir=tmp,
                )

        self.assertEqual(captured["batch_lengths"], [1])

    @unittest.skipIf(pa is None, "pyarrow is required for SDK data ingestion tests")
    def test_mmap_transport_rejects_managed_process_input(self):
        with self.assertRaisesRegex(ValueError, "stdin/input are managed"):
            ingestr.ingest(
                [{"id": 1}],
                dest_uri="sqlite:///tmp/out.db",
                dest_table="main.rows",
                transport="mmap",
                input=b"",
            )

        with self.assertRaisesRegex(ValueError, "stdin/input are managed"):
            ingestr.ingest(
                dest_uri="sqlite:///tmp/out.db",
                dest_table="main.rows",
                transport="mmap",
                stdin=subprocess.PIPE,
            )

    @unittest.skipIf(pa is None, "pyarrow is required for SDK data ingestion tests")
    def test_context_manager_preserves_result_when_stream_close_fails(self):
        fake = None

        def popen(*args, **kwargs):
            nonlocal fake
            fake = FakePopen(*args, **kwargs)
            fake.returncode = 1
            return fake

        with patch("ingestr._data.binary_path", return_value="/tmp/ingestr"):
            with patch("subprocess.Popen", side_effect=popen):
                with self.assertRaises(subprocess.CalledProcessError):
                    with ingestr.ingest(dest_uri="sqlite:///tmp/out.db", dest_table="main.rows") as send:
                        send({"id": 1})

        self.assertTrue(send._closed)
        self.assertIsNotNone(send.result)
        self.assertEqual(send.result.returncode, 1)

    @unittest.skipIf(pa is None, "pyarrow is required for SDK data ingestion tests")
    def test_ingest_normalizes_common_python_row_values(self):
        @dataclass
        class Row:
            id: int
            created_at: datetime

        fake = None

        def popen(*args, **kwargs):
            nonlocal fake
            fake = FakePopen(*args, **kwargs)
            return fake

        with patch("ingestr._data.binary_path", return_value="/tmp/ingestr"):
            with patch("subprocess.Popen", side_effect=popen):
                ingestr.ingest(
                    [Row(id=1, created_at=datetime(2026, 1, 2, 3, 4, 5))],
                    dest_uri="sqlite:///tmp/out.db",
                    dest_table="main.rows",
                )

        table = pa.ipc.open_stream(pa.BufferReader(fake.stdin_buffer.getvalue())).read_all()
        self.assertEqual(table.to_pylist(), [{"id": 1, "created_at": datetime(2026, 1, 2, 3, 4, 5)}])

    @unittest.skipIf(pa is None, "pyarrow is required for SDK data ingestion tests")
    def test_stream_transport_rejects_text_mode(self):
        with self.assertRaises(ValueError):
            ingestr.ingest(
                pa.table({"id": [1]}),
                dest_uri="sqlite:///tmp/out.db",
                dest_table="main.df",
                text=True,
            )


if __name__ == "__main__":
    unittest.main()

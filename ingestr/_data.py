from __future__ import annotations

import dataclasses
import inspect
import math
import os
import subprocess
import tempfile
import threading
from collections.abc import Iterable, Iterator, Mapping
from contextlib import contextmanager
from pathlib import Path
from typing import Any, Optional

from ._runner import PathLike, binary_path, build_ingest_args, ingest as _cli_ingest

Transport = str
_MISSING = object()


def ingest(
    data: Any = _MISSING,
    *,
    dest_uri: str,
    dest_table: str,
    source_table: str = "python_data",
    transport: Transport = "stream",
    batch_size: int = 10000,
    schema: Any = None,
    **options: Any,
) -> Any:
    """Ingest Python data using Arrow IPC stream by default.

    When data is omitted, returns a context manager that accepts rows, pages,
    generators, DataFrames, PyArrow tables, and record batches inside the block.
    """

    if data is _MISSING:
        return IngestSession(
            dest_uri=dest_uri,
            dest_table=dest_table,
            source_table=source_table,
            transport=transport,
            batch_size=batch_size,
            schema=schema,
            **options,
        )

    pa = _require_pyarrow()
    batches, arrow_schema = _batches_from_input(data, pa=pa, batch_size=batch_size, schema=schema)
    return _ingest_batches(
        batches,
        pa=pa,
        schema=arrow_schema,
        dest_uri=dest_uri,
        dest_table=dest_table,
        source_table=source_table,
        transport=transport,
        **options,
    )


class IngestSession:
    """Context manager for push-style Python data ingestion."""

    def __init__(
        self,
        *,
        dest_uri: str,
        dest_table: str,
        source_table: str = "python_data",
        transport: Transport = "stream",
        batch_size: int = 10000,
        schema: Any = None,
        **options: Any,
    ) -> None:
        self.dest_uri = dest_uri
        self.dest_table = dest_table
        self.source_table = source_table
        self.transport = transport.lower()
        self.batch_size = batch_size
        self.result: Optional[subprocess.CompletedProcess] = None

        if self.transport not in {"stream", "mmap"}:
            raise ValueError("transport must be 'stream' or 'mmap'")

        self._pa = None
        self._schema = schema
        self._closed = False
        self._saw_rows = False
        self._writer = None
        self._proc = None
        self._command = None
        self._drainer = None
        self._writer_error: Optional[BaseException] = None
        self._temp_path: Optional[str] = None

        self._cli_options = dict(options)
        self._process_options = _extract_process_options(self._cli_options)
        self._temp_dir = self._process_options.pop("temp_dir", None)
        self._keep_temp_file = bool(self._process_options.pop("keep_temp_file", False))

        self._stream_check = True
        self._stream_executable = None
        if self.transport == "stream":
            self._prepare_stream_process_options()
        else:
            _reject_managed_process_input(self._process_options)

    def __enter__(self) -> "IngestSession":
        self._ensure_pyarrow()
        return self

    def __exit__(self, exc_type: Any, exc: Any, tb: Any) -> bool:
        if exc_type is not None:
            self._abort()
            return False

        self.close()
        return False

    def __call__(self, data: Any) -> "IngestSession":
        return self.ingest(data)

    def ingest(self, data: Any) -> "IngestSession":
        if self._closed:
            raise ValueError("ingestion session is already closed")

        pa = self._ensure_pyarrow()
        batches, arrow_schema = _batches_from_input(data, pa=pa, batch_size=self.batch_size, schema=self._schema)
        first_batch, remaining = _peek_first_batch(batches)
        if first_batch is None:
            return self

        if self._schema is None:
            self._schema = arrow_schema or first_batch.schema

        self._saw_rows = True
        self._write_batches(_prepend(first_batch, remaining))
        return self

    def close(self) -> subprocess.CompletedProcess:
        if self._closed:
            if self.result is None:
                raise ValueError("ingestion session closed before producing a result")
            return self.result

        self._closed = True
        if not self._saw_rows:
            self._cleanup_temp_file()
            raise ValueError("input produced no rows")

        if self.transport == "stream":
            self.result = self._close_stream()
        else:
            self.result = self._close_mmap()
        return self.result

    def _ensure_pyarrow(self) -> Any:
        if self._pa is None:
            self._pa = _require_pyarrow()
        return self._pa

    def _prepare_stream_process_options(self) -> None:
        self._stream_check = self._process_options.pop("check", True)
        self._stream_executable = self._process_options.pop("executable", None)
        capture_output = self._process_options.pop("capture_output", False)

        if capture_output:
            if "stdout" in self._process_options or "stderr" in self._process_options:
                raise ValueError("stdout and stderr may not be used with capture_output")
            self._process_options["stdout"] = subprocess.PIPE
            self._process_options["stderr"] = subprocess.PIPE

        if self._process_options.get("text") or self._process_options.get("universal_newlines"):
            raise ValueError("Arrow stream ingestion requires binary stdin; text mode is not supported")
        if "stdin" in self._process_options or "input" in self._process_options:
            raise ValueError("stdin/input are managed by ingestr's Arrow stream transport")

    def _write_batches(self, batches: Iterable[Any]) -> None:
        if self.transport == "stream":
            self._start_stream()
        else:
            self._start_mmap()

        assert self._writer is not None
        try:
            _write_non_empty_batches(self._writer, batches)
        except BrokenPipeError as exc:
            self._writer_error = exc
            self.close()
        except BaseException:
            self._abort()
            raise

    def _start_stream(self) -> None:
        if self._writer is not None:
            return

        assert self._schema is not None
        command = [
            os.fspath(self._stream_executable) if self._stream_executable is not None else binary_path(),
            *build_ingest_args(
                source_uri="arrow-stream://-",
                source_table=self.source_table,
                dest_uri=self.dest_uri,
                dest_table=self.dest_table,
                **self._cli_options,
            ),
        ]
        self._command = command
        self._proc = subprocess.Popen(command, stdin=subprocess.PIPE, **self._process_options)
        self._drainer = _ProcessOutputDrainer(self._proc)
        assert self._proc.stdin is not None
        self._writer = self._pa.ipc.new_stream(self._proc.stdin, self._schema)

    def _start_mmap(self) -> None:
        if self._writer is not None:
            return

        assert self._schema is not None
        fd, path = tempfile.mkstemp(
            prefix="ingestr-python-",
            suffix=".arrow",
            dir=_optional_fspath(self._temp_dir),
        )
        os.close(fd)
        self._temp_path = path
        self._writer = self._pa.ipc.new_file(path, self._schema)

    def _close_stream(self) -> subprocess.CompletedProcess:
        assert self._proc is not None
        assert self._command is not None

        if self._writer is not None:
            try:
                self._writer.close()
            except BrokenPipeError as exc:
                self._writer_error = self._writer_error or exc
            except BaseException:
                self._abort()
                raise
            self._writer = None

        if self._proc.stdin is not None:
            try:
                self._proc.stdin.close()
            except BrokenPipeError as exc:
                self._writer_error = self._writer_error or exc
            self._proc.stdin = None

        returncode = self._proc.wait()
        stdout, stderr = self._drainer.collect() if self._drainer is not None else (None, None)
        completed = subprocess.CompletedProcess(self._command, returncode, stdout, stderr)
        self.result = completed

        if self._stream_check and completed.returncode:
            raise subprocess.CalledProcessError(completed.returncode, self._command, output=stdout, stderr=stderr)
        if self._writer_error is not None and completed.returncode == 0:
            raise self._writer_error

        return completed

    def _close_mmap(self) -> subprocess.CompletedProcess:
        assert self._temp_path is not None
        assert self._writer is not None

        try:
            self._writer.close()
            self._writer = None
            return _cli_ingest(
                source_uri=f"mmap://{self._temp_path}",
                source_table=self.source_table,
                dest_uri=self.dest_uri,
                dest_table=self.dest_table,
                **self._cli_options,
                **self._process_options,
            )
        finally:
            self._cleanup_temp_file()

    def _abort(self) -> None:
        try:
            if self._writer is not None:
                self._writer.close()
        except Exception:
            pass
        finally:
            self._writer = None

        if self._proc is not None:
            poll = getattr(self._proc, "poll", None)
            if poll is None or poll() is None:
                self._proc.kill()
                self._proc.wait()
            if self._drainer is not None:
                self._drainer.collect()

        self._cleanup_temp_file()
        self._closed = True

    def _cleanup_temp_file(self) -> None:
        if self._temp_path is None or self._keep_temp_file:
            return
        try:
            os.remove(self._temp_path)
        except FileNotFoundError:
            pass
        finally:
            self._temp_path = None


def _ingest_batches(
    batches: Iterable[Any],
    *,
    pa: Any,
    dest_uri: str,
    dest_table: str,
    source_table: str,
    transport: Transport,
    schema: Any = None,
    **options: Any,
) -> subprocess.CompletedProcess:
    first_batch, remaining = _peek_first_batch(batches)
    if first_batch is None:
        raise ValueError("input produced no rows")

    arrow_schema = schema or first_batch.schema
    all_batches = _prepend(first_batch, remaining)

    normalized_transport = transport.lower()
    if normalized_transport == "stream":
        return _ingest_stream(
            all_batches,
            pa=pa,
            schema=arrow_schema,
            dest_uri=dest_uri,
            dest_table=dest_table,
            source_table=source_table,
            **options,
        )
    if normalized_transport == "mmap":
        return _ingest_mmap(
            all_batches,
            pa=pa,
            schema=arrow_schema,
            dest_uri=dest_uri,
            dest_table=dest_table,
            source_table=source_table,
            **options,
        )
    raise ValueError("transport must be 'stream' or 'mmap'")


def _ingest_stream(
    batches: Iterable[Any],
    *,
    pa: Any,
    schema: Any,
    dest_uri: str,
    dest_table: str,
    source_table: str,
    **options: Any,
) -> subprocess.CompletedProcess:
    process_options = _extract_process_options(options)
    check = process_options.pop("check", True)
    executable = process_options.pop("executable", None)
    capture_output = process_options.pop("capture_output", False)
    process_options.pop("temp_dir", None)
    process_options.pop("keep_temp_file", None)

    if capture_output:
        if "stdout" in process_options or "stderr" in process_options:
            raise ValueError("stdout and stderr may not be used with capture_output")
        process_options["stdout"] = subprocess.PIPE
        process_options["stderr"] = subprocess.PIPE

    if process_options.get("text") or process_options.get("universal_newlines"):
        raise ValueError("Arrow stream ingestion requires binary stdin; text mode is not supported")
    if "stdin" in process_options or "input" in process_options:
        raise ValueError("stdin/input are managed by ingestr's Arrow stream transport")

    command = [
        os.fspath(executable) if executable is not None else binary_path(),
        *build_ingest_args(
            source_uri="arrow-stream://-",
            source_table=source_table,
            dest_uri=dest_uri,
            dest_table=dest_table,
            **options,
        ),
    ]

    proc = subprocess.Popen(command, stdin=subprocess.PIPE, **process_options)
    drainer = _ProcessOutputDrainer(proc)
    assert proc.stdin is not None

    writer_error: Optional[BaseException] = None
    try:
        with pa.ipc.new_stream(proc.stdin, schema) as writer:
            _write_non_empty_batches(writer, batches)
    except BrokenPipeError as exc:
        writer_error = exc
    except BaseException:
        proc.kill()
        proc.wait()
        drainer.collect()
        raise
    finally:
        try:
            proc.stdin.close()
        except BrokenPipeError as exc:
            writer_error = writer_error or exc
        proc.stdin = None

    returncode = proc.wait()
    stdout, stderr = drainer.collect()
    completed = subprocess.CompletedProcess(command, returncode, stdout, stderr)

    if check and completed.returncode:
        raise subprocess.CalledProcessError(completed.returncode, command, output=stdout, stderr=stderr)
    if writer_error is not None and completed.returncode == 0:
        raise writer_error

    return completed


def _ingest_mmap(
    batches: Iterable[Any],
    *,
    pa: Any,
    schema: Any,
    dest_uri: str,
    dest_table: str,
    source_table: str,
    **options: Any,
) -> subprocess.CompletedProcess:
    process_options = _extract_process_options(options)
    temp_dir = process_options.pop("temp_dir", None)
    keep_temp_file = bool(process_options.pop("keep_temp_file", False))
    _reject_managed_process_input(process_options)

    with _temporary_arrow_file(temp_dir=temp_dir, keep=keep_temp_file) as path:
        with pa.ipc.new_file(path, schema) as writer:
            _write_non_empty_batches(writer, batches)

        return _cli_ingest(
            source_uri=f"mmap://{path}",
            source_table=source_table,
            dest_uri=dest_uri,
            dest_table=dest_table,
            **options,
            **process_options,
        )


def _batches_from_input(data: Any, *, pa: Any, batch_size: int, schema: Any = None) -> tuple[Iterator[Any], Any]:
    data = _resolve_callable_input(data)

    if _is_row_like(data):
        return _iterable_batches([data], pa=pa, batch_size=batch_size, schema=schema), schema

    try:
        table = _to_arrow_table(data, pa=pa, schema=schema)
    except TypeError:
        return _iterable_batches(data, pa=pa, batch_size=batch_size, schema=schema), schema

    return iter(table.to_batches(max_chunksize=batch_size)), table.schema


def _resolve_callable_input(data: Any) -> Any:
    if not callable(data):
        return data

    if inspect.iscoroutinefunction(data) or inspect.isasyncgenfunction(data):
        raise TypeError("async data functions are not supported; pass a synchronous iterable instead")

    resolved = data()
    if inspect.isawaitable(resolved):
        close = getattr(resolved, "close", None)
        if close is not None:
            close()
        raise TypeError("async data functions are not supported; pass a synchronous iterable instead")
    if hasattr(resolved, "__aiter__"):
        raise TypeError("async data iterables are not supported; pass a synchronous iterable instead")

    return resolved


def _iterable_batches(data: Iterable[Any], *, pa: Any, batch_size: int, schema: Any = None) -> Iterator[Any]:
    rows = []

    def flush_rows() -> Iterator[Any]:
        nonlocal rows, schema
        if not rows:
            return
        if schema is None:
            batch = pa.RecordBatch.from_pylist(rows)
            schema = batch.schema
        else:
            batch = pa.RecordBatch.from_pylist(rows, schema=schema)
        rows = []
        yield batch

    for item in data:
        if item is None:
            continue

        if _is_row_like(item):
            rows.append(_normalize_row(item))
            if len(rows) >= batch_size:
                yield from flush_rows()
            continue

        yield from flush_rows()

        try:
            table = _to_arrow_table(item, pa=pa, schema=schema)
        except TypeError:
            for row in item:
                rows.append(_normalize_row(row))
                if len(rows) >= batch_size:
                    yield from flush_rows()
            continue

        if table.num_rows == 0:
            continue
        if schema is None:
            schema = table.schema
        for batch in table.to_batches(max_chunksize=batch_size):
            yield batch

    yield from flush_rows()


def _to_arrow_table(data: Any, *, pa: Any, schema: Any = None) -> Any:
    if isinstance(data, pa.Table):
        if schema is not None and not data.schema.equals(schema):
            return data.cast(schema)
        return data

    if isinstance(data, pa.RecordBatch):
        table = pa.Table.from_batches([data])
        if schema is not None and not table.schema.equals(schema):
            return table.cast(schema)
        return table

    if hasattr(pa, "RecordBatchReader") and isinstance(data, pa.RecordBatchReader):
        return data.read_all()

    if hasattr(data, "to_arrow"):
        table = data.to_arrow()
        if isinstance(table, pa.Table):
            if schema is not None and not table.schema.equals(schema):
                return table.cast(schema)
            return table

    if hasattr(data, "to_pandas") and type(data).__module__.startswith("pyarrow"):
        return pa.Table.from_pandas(data.to_pandas(), schema=schema, preserve_index=False)

    if _looks_like_pandas_dataframe(data):
        return pa.Table.from_pandas(data, schema=schema, preserve_index=False)

    if hasattr(data, "to_dicts"):
        return pa.Table.from_pylist([_normalize_row(row) for row in data.to_dicts()], schema=schema)

    if hasattr(data, "to_dict"):
        try:
            rows = data.to_dict(orient="records")
        except TypeError as exc:
            raise TypeError(f"unsupported dataframe type: {type(data).__name__}") from exc
        return pa.Table.from_pylist([_normalize_row(row) for row in rows], schema=schema)

    raise TypeError(f"unsupported dataframe type: {type(data).__name__}")


def _normalize_row(row: Any) -> Mapping[str, Any]:
    if isinstance(row, Mapping):
        raw = row
    elif dataclasses.is_dataclass(row):
        raw = dataclasses.asdict(row)
    elif hasattr(row, "model_dump"):
        raw = row.model_dump()
    elif hasattr(row, "_asdict"):
        raw = row._asdict()
    elif hasattr(row, "__dict__"):
        raw = vars(row)
    else:
        raise TypeError(f"row must be mapping-like, got {type(row).__name__}")

    if not isinstance(raw, Mapping):
        raise TypeError(f"row conversion must produce a mapping, got {type(raw).__name__}")

    return {str(key): _normalize_value(value) for key, value in raw.items()}


def _is_row_like(value: Any) -> bool:
    return (
        isinstance(value, Mapping)
        or dataclasses.is_dataclass(value)
        or hasattr(value, "model_dump")
        or hasattr(value, "_asdict")
        or (hasattr(value, "__dict__") and not _looks_like_dataframe_or_arrow(value))
    )


def _normalize_value(value: Any) -> Any:
    if _is_null(value):
        return None
    item = getattr(value, "item", None)
    if item is not None:
        try:
            return item()
        except Exception:
            return value
    return value


def _is_null(value: Any) -> bool:
    if value is None:
        return True
    if isinstance(value, float):
        return math.isnan(value)
    try:
        import pandas
    except Exception:
        return False

    try:
        result = pandas.isna(value)
    except Exception:
        return False
    return isinstance(result, bool) and result


def _peek_first_batch(batches: Iterable[Any]) -> tuple[Optional[Any], Iterator[Any]]:
    iterator = iter(batches)
    for batch in iterator:
        if batch is not None and batch.num_rows > 0:
            return batch, iterator
    return None, iter(())


def _prepend(first: Any, rest: Iterable[Any]) -> Iterator[Any]:
    yield first
    yield from rest


def _write_non_empty_batches(writer: Any, batches: Iterable[Any]) -> None:
    for batch in batches:
        if batch is not None and batch.num_rows > 0:
            writer.write_batch(batch)


def _looks_like_pandas_dataframe(value: Any) -> bool:
    return type(value).__module__.startswith("pandas.") or type(value).__module__ == "pandas.core.frame"


def _looks_like_dataframe_or_arrow(value: Any) -> bool:
    if _looks_like_pandas_dataframe(value):
        return True
    module = type(value).__module__
    return module.startswith("pyarrow") or hasattr(value, "to_arrow") or hasattr(value, "to_dicts")


def _require_pyarrow() -> Any:
    try:
        import pyarrow as pa
    except ImportError as exc:
        raise ImportError(
            "Python data ingestion requires pyarrow. Install it with `pip install 'ingestr[sdk]'` "
            "or `pip install pyarrow`."
        ) from exc
    return pa


def _extract_process_options(options: dict[str, Any]) -> dict[str, Any]:
    keys = {
        "check",
        "executable",
        "capture_output",
        "cwd",
        "env",
        "stdout",
        "stderr",
        "text",
        "universal_newlines",
        "temp_dir",
        "keep_temp_file",
        "stdin",
        "input",
    }
    out = {}
    for key in list(options):
        if key in keys:
            out[key] = options.pop(key)
    return out


def _reject_managed_process_input(process_options: Mapping[str, Any]) -> None:
    if "stdin" in process_options or "input" in process_options:
        raise ValueError("stdin/input are managed by ingestr's Python data transport")


@contextmanager
def _temporary_arrow_file(*, temp_dir: Optional[PathLike], keep: bool) -> Iterator[str]:
    fd, path = tempfile.mkstemp(prefix="ingestr-python-", suffix=".arrow", dir=_optional_fspath(temp_dir))
    os.close(fd)

    try:
        yield path
    finally:
        if not keep:
            try:
                os.remove(path)
            except FileNotFoundError:
                pass


def _optional_fspath(path: Optional[PathLike]) -> Optional[str]:
    if path is None:
        return None
    return os.fspath(Path(path))


class _ProcessOutputDrainer:
    def __init__(self, proc: subprocess.Popen) -> None:
        self._stdout = _PipeCapture(getattr(proc, "stdout", None))
        self._stderr = _PipeCapture(getattr(proc, "stderr", None))

    def collect(self) -> tuple[Any, Any]:
        return self._stdout.collect(), self._stderr.collect()


class _PipeCapture:
    def __init__(self, stream: Any) -> None:
        self._stream = stream
        self._chunks = []
        self._thread = None

        if stream is not None:
            self._thread = threading.Thread(target=self._read, daemon=True)
            self._thread.start()

    def collect(self) -> Any:
        if self._thread is None:
            return None
        self._thread.join()
        if not self._chunks:
            return b""
        first = self._chunks[0]
        if isinstance(first, str):
            return "".join(self._chunks)
        return b"".join(self._chunks)

    def _read(self) -> None:
        try:
            while True:
                chunk = self._stream.read(8192)
                if not chunk:
                    break
                self._chunks.append(chunk)
        finally:
            try:
                self._stream.close()
            except Exception:
                pass

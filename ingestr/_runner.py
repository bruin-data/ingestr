from __future__ import annotations

import json
import os
import shutil
import subprocess
import sys
import sysconfig
from collections.abc import Mapping, Sequence
from datetime import date, datetime
from pathlib import Path
from typing import Any, List, Optional, Union

PathLike = Union[str, os.PathLike]


class IngestrNotFoundError(FileNotFoundError):
    """Raised when the bundled ingestr executable cannot be found."""

    pass


def binary_path() -> str:
    """Return the path to the installed ingestr executable."""

    for candidate in _binary_candidates():
        if candidate.is_file():
            return str(candidate)

    for name in _binary_names():
        resolved = shutil.which(name)
        if resolved:
            return resolved

    checked = ", ".join(str(path) for path in _binary_candidates())
    raise IngestrNotFoundError(
        "could not find the ingestr executable; reinstall with `pip install ingestr` "
        "or build it locally with `make build`. Checked: " + checked
    )


def run(
    args: Optional[Sequence[object]] = None,
    *,
    check: bool = True,
    executable: Optional[PathLike] = None,
    **kwargs: Any,
) -> subprocess.CompletedProcess:
    """Run the ingestr CLI with subprocess.run and return its CompletedProcess."""

    normalized_args = _normalize_args(args)
    command = [os.fspath(executable) if executable is not None else binary_path()]
    command.extend(normalized_args)
    return subprocess.run(command, check=check, **kwargs)


def ingest(
    *,
    source_uri: str,
    dest_uri: str,
    source_table: Optional[str] = None,
    dest_table: Optional[str] = None,
    incremental_key: Optional[str] = None,
    incremental_strategy: Optional[str] = None,
    interval_start: Optional[Union[str, date, datetime]] = None,
    interval_end: Optional[Union[str, date, datetime]] = None,
    primary_key: Optional[Union[str, Sequence[str]]] = None,
    partition_by: Optional[str] = None,
    cluster_by: Optional[Union[str, Sequence[str]]] = None,
    yes: bool = False,
    full_refresh: bool = False,
    schema_contract: Optional[str] = None,
    schema_naming: Optional[str] = None,
    progress: Optional[str] = None,
    page_size: Optional[int] = None,
    loader_file_size: Optional[int] = None,
    loader_file_format: Optional[str] = None,
    extract_parallelism: Optional[int] = None,
    sql_limit: Optional[int] = None,
    sql_exclude_columns: Optional[Union[str, Sequence[str]]] = None,
    sql_backend: Optional[Union[str, Sequence[str]]] = None,
    columns: Optional[str] = None,
    no_inference: bool = False,
    mask: Optional[Union[str, Sequence[str]]] = None,
    pipelines_dir: Optional[PathLike] = None,
    staging_bucket: Optional[str] = None,
    staging_dataset: Optional[str] = None,
    debug: bool = False,
    query_annotations: Optional[Union[str, Mapping[str, Any]]] = None,
    extra_args: Optional[Sequence[object]] = None,
    check: bool = True,
    executable: Optional[PathLike] = None,
    **run_kwargs: Any,
) -> subprocess.CompletedProcess:
    """Run `ingestr ingest` using Python keyword arguments for CLI flags."""

    args = build_ingest_args(
        source_uri=source_uri,
        dest_uri=dest_uri,
        source_table=source_table,
        dest_table=dest_table,
        incremental_key=incremental_key,
        incremental_strategy=incremental_strategy,
        interval_start=interval_start,
        interval_end=interval_end,
        primary_key=primary_key,
        partition_by=partition_by,
        cluster_by=cluster_by,
        yes=yes,
        full_refresh=full_refresh,
        schema_contract=schema_contract,
        schema_naming=schema_naming,
        progress=progress,
        page_size=page_size,
        loader_file_size=loader_file_size,
        loader_file_format=loader_file_format,
        extract_parallelism=extract_parallelism,
        sql_limit=sql_limit,
        sql_exclude_columns=sql_exclude_columns,
        sql_backend=sql_backend,
        columns=columns,
        no_inference=no_inference,
        mask=mask,
        pipelines_dir=pipelines_dir,
        staging_bucket=staging_bucket,
        staging_dataset=staging_dataset,
        debug=debug,
        query_annotations=query_annotations,
        extra_args=extra_args,
    )
    return run(args, check=check, executable=executable, **run_kwargs)


def build_ingest_args(
    *,
    source_uri: str,
    dest_uri: str,
    source_table: Optional[str] = None,
    dest_table: Optional[str] = None,
    incremental_key: Optional[str] = None,
    incremental_strategy: Optional[str] = None,
    interval_start: Optional[Union[str, date, datetime]] = None,
    interval_end: Optional[Union[str, date, datetime]] = None,
    primary_key: Optional[Union[str, Sequence[str]]] = None,
    partition_by: Optional[str] = None,
    cluster_by: Optional[Union[str, Sequence[str]]] = None,
    yes: bool = False,
    full_refresh: bool = False,
    schema_contract: Optional[str] = None,
    schema_naming: Optional[str] = None,
    progress: Optional[str] = None,
    page_size: Optional[int] = None,
    loader_file_size: Optional[int] = None,
    loader_file_format: Optional[str] = None,
    extract_parallelism: Optional[int] = None,
    sql_limit: Optional[int] = None,
    sql_exclude_columns: Optional[Union[str, Sequence[str]]] = None,
    sql_backend: Optional[Union[str, Sequence[str]]] = None,
    columns: Optional[str] = None,
    no_inference: bool = False,
    mask: Optional[Union[str, Sequence[str]]] = None,
    pipelines_dir: Optional[PathLike] = None,
    staging_bucket: Optional[str] = None,
    staging_dataset: Optional[str] = None,
    debug: bool = False,
    query_annotations: Optional[Union[str, Mapping[str, Any]]] = None,
    extra_args: Optional[Sequence[object]] = None,
) -> List[str]:
    """Build CLI arguments for `ingestr ingest` without executing the command."""

    args = ["ingest"]
    _append_option(args, "source-uri", source_uri)
    _append_option(args, "dest-uri", dest_uri)
    _append_option(args, "source-table", source_table)
    _append_option(args, "dest-table", dest_table)
    _append_option(args, "incremental-key", incremental_key)
    _append_option(args, "incremental-strategy", incremental_strategy)
    _append_option(args, "interval-start", interval_start)
    _append_option(args, "interval-end", interval_end)
    _append_repeated(args, "primary-key", primary_key)
    _append_option(args, "partition-by", partition_by)
    _append_csv(args, "cluster-by", cluster_by)
    _append_bool(args, "yes", yes)
    _append_bool(args, "full-refresh", full_refresh)
    _append_option(args, "schema-contract", schema_contract)
    _append_option(args, "schema-naming", schema_naming)
    _append_option(args, "progress", progress)
    _append_option(args, "page-size", page_size)
    _append_option(args, "loader-file-size", loader_file_size)
    _append_option(args, "loader-file-format", loader_file_format)
    _append_option(args, "extract-parallelism", extract_parallelism)
    _append_option(args, "sql-limit", sql_limit)
    _append_repeated(args, "sql-exclude-columns", sql_exclude_columns)
    _append_repeated(args, "sql-backend", sql_backend)
    _append_option(args, "columns", columns)
    _append_bool(args, "no-inference", no_inference)
    _append_repeated(args, "mask", mask)
    _append_option(args, "pipelines-dir", pipelines_dir)
    _append_option(args, "staging-bucket", staging_bucket)
    _append_option(args, "staging-dataset", staging_dataset)
    _append_bool(args, "debug", debug)
    _append_option(args, "query-annotations", _format_query_annotations(query_annotations))
    args.extend(_normalize_args(extra_args))
    return args


def main(argv: Optional[Sequence[object]] = None) -> int:
    """Entry point used by `python -m ingestr`."""

    try:
        completed = run(sys.argv[1:] if argv is None else argv, check=False)
    except IngestrNotFoundError as exc:
        print(str(exc), file=sys.stderr)
        return 1
    return completed.returncode


def _binary_names() -> Sequence[str]:
    if os.name == "nt":
        return ("ingestr.exe", "ingestr")
    return ("ingestr", "ingestr.exe")


def _binary_candidates() -> List[Path]:
    dirs = _binary_dirs()
    seen = set()
    candidates: List[Path] = []
    for directory in dirs:
        for name in _binary_names():
            candidate = directory / name
            marker = str(candidate)
            if marker not in seen:
                seen.add(marker)
                candidates.append(candidate)
    return candidates


def _binary_dirs() -> List[Path]:
    dirs: List[Path] = []
    dirs.append(Path(__file__).resolve().parents[1] / "bin")

    scripts_dir = sysconfig.get_path("scripts")
    if scripts_dir:
        dirs.append(Path(scripts_dir))

    if sys.executable:
        dirs.append(Path(sys.executable).resolve().parent)
    return dirs


def _normalize_args(args: Optional[Sequence[object]]) -> List[str]:
    if args is None:
        return []
    if isinstance(args, (str, bytes)):
        raise TypeError("args must be a sequence of arguments, not a shell command string")
    return [_stringify(arg) for arg in args]


def _append_option(args: List[str], name: str, value: Any) -> None:
    if value is None:
        return
    args.extend([f"--{name}", _stringify(value)])


def _append_bool(args: List[str], name: str, value: bool) -> None:
    if value:
        args.append(f"--{name}")


def _append_repeated(args: List[str], name: str, values: Any) -> None:
    if values is None:
        return
    if _is_scalar(values):
        iterable = [values]
    else:
        iterable = values

    for value in iterable:
        if value is not None:
            _append_option(args, name, value)


def _append_csv(args: List[str], name: str, values: Any) -> None:
    if values is None:
        return
    if _is_scalar(values):
        _append_option(args, name, values)
        return
    _append_option(args, name, ",".join(_stringify(value) for value in values))


def _is_scalar(value: Any) -> bool:
    return isinstance(value, (str, bytes, os.PathLike))


def _stringify(value: Any) -> str:
    if isinstance(value, datetime):
        return value.isoformat()
    if isinstance(value, date):
        return value.isoformat()
    if isinstance(value, os.PathLike):
        return os.fspath(value)
    return str(value)


def _format_query_annotations(value: Any) -> Any:
    if isinstance(value, Mapping):
        return json.dumps(value, sort_keys=True, separators=(",", ":"))
    return value

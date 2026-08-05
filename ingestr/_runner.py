from __future__ import annotations

import hashlib
import json
import os
import platform
import shutil
import stat
import subprocess
import sys
import tarfile
import tempfile
import urllib.parse
import urllib.request
import zipfile
from collections.abc import Mapping, Sequence
from datetime import date, datetime
from pathlib import Path
from typing import Any, List, NamedTuple, Optional, Union

from ._checksums import ARCHIVE_SHA256

try:
    from importlib.metadata import PackageNotFoundError, version
except ModuleNotFoundError:
    from importlib_metadata import PackageNotFoundError, version  # type: ignore

PathLike = Union[str, os.PathLike]
_GITHUB_RELEASE_BASE_URL = "https://github.com/bruin-data/ingestr/releases/download"
_BINARY_PATH_ENV = "INGESTR_BINARY_PATH"
_BINARY_CACHE_DIR_ENV = "INGESTR_BINARY_CACHE_DIR"
_BINARY_TAG_ENV = "INGESTR_BINARY_TAG"
_DOWNLOAD_TIMEOUT_SECONDS = 60


class IngestrNotFoundError(FileNotFoundError):
    """Raised when the ingestr executable cannot be found or installed."""

    pass


class _ReleasePlatform(NamedTuple):
    os_name: str
    arch: str
    archive_suffix: str
    binary_name: str


def binary_path() -> str:
    """Return the path to the ingestr executable, downloading it if needed."""

    override = _binary_path_override()
    if override is not None:
        return str(override)

    local = _local_binary_path()
    if local is not None:
        return str(local)

    cached = _cached_binary_path()
    if cached.is_file():
        _ensure_executable(cached)
        return str(cached)

    return str(_download_binary(cached))


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
    incremental_predicate: Optional[str] = None,
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
    trim_whitespace: bool = False,
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
        incremental_predicate=incremental_predicate,
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
        trim_whitespace=trim_whitespace,
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
    incremental_predicate: Optional[str] = None,
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
    trim_whitespace: bool = False,
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
    _append_option(args, "incremental-predicate", incremental_predicate)
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
    _append_bool(args, "trim-whitespace", trim_whitespace)
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


def _binary_path_override() -> Optional[Path]:
    configured = os.environ.get(_BINARY_PATH_ENV)
    if not configured:
        return None

    candidate = Path(os.path.expandvars(configured)).expanduser()
    if candidate.is_file():
        _ensure_executable(candidate)
        return candidate

    raise IngestrNotFoundError("%s points to a missing ingestr executable: %s" % (_BINARY_PATH_ENV, candidate))


def _local_binary_path() -> Optional[Path]:
    for directory in _local_binary_dirs():
        for name in _binary_names():
            candidate = directory / name
            if candidate.is_file():
                _ensure_executable(candidate)
                return candidate
    return None


def _local_binary_dirs() -> List[Path]:
    return [Path(__file__).resolve().parents[1] / "bin"]


def _cached_binary_path() -> Path:
    release = _release_platform()
    platform_dir = "%s_%s" % (release.os_name, release.arch)
    return _cache_root() / "bin" / _release_tag() / platform_dir / release.binary_name


def _download_binary(target: Path) -> Path:
    release = _release_platform()
    tag = _release_tag()
    archive_name = _release_archive_name(release)
    url = _release_asset_url(tag, archive_name)
    target.parent.mkdir(parents=True, exist_ok=True)

    temp_dir = Path(tempfile.mkdtemp(prefix="ingestr-download-", dir=str(target.parent)))
    try:
        archive_path = temp_dir / archive_name
        extracted_path = temp_dir / release.binary_name
        _download_file(url, archive_path)
        _verify_archive_checksum(archive_path, tag, archive_name)
        _extract_binary(archive_path, release.binary_name, extracted_path)
        _ensure_executable(extracted_path)
        os.replace(str(extracted_path), str(target))
        _ensure_executable(target)
        return target
    except Exception as exc:
        raise IngestrNotFoundError(
            "failed to download ingestr %s for %s/%s from %s: %s"
            % (tag, release.os_name, release.arch, url, exc)
        ) from exc
    finally:
        shutil.rmtree(str(temp_dir), ignore_errors=True)


def _download_file(url: str, destination: Path) -> None:
    request = urllib.request.Request(
        url,
        headers={"User-Agent": "ingestr-python/%s" % _package_version()},
    )
    with urllib.request.urlopen(request, timeout=_DOWNLOAD_TIMEOUT_SECONDS) as response:
        with destination.open("wb") as output:
            shutil.copyfileobj(response, output)


def _verify_archive_checksum(archive_path: Path, tag: str, archive_name: str) -> None:
    expected = _archive_checksum(tag, archive_name)
    if not expected:
        raise IngestrNotFoundError(
            "no embedded SHA256 checksum for %s/%s; set %s to use a trusted local binary"
            % (tag, archive_name, _BINARY_PATH_ENV)
        )

    actual = _sha256_file(archive_path)
    if actual.lower() != expected.lower():
        raise IngestrNotFoundError(
            "downloaded archive checksum mismatch for %s: expected %s, got %s"
            % (archive_name, expected, actual)
        )


def _archive_checksum(tag: str, archive_name: str) -> Optional[str]:
    checksums = ARCHIVE_SHA256.get(tag)
    if not isinstance(checksums, Mapping):
        return None

    expected = checksums.get(archive_name)
    if not isinstance(expected, str):
        return None
    return expected


def _sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as input_file:
        for chunk in iter(lambda: input_file.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def _extract_binary(archive_path: Path, binary_name: str, destination: Path) -> None:
    if archive_path.suffix == ".zip":
        _extract_binary_from_zip(archive_path, binary_name, destination)
        return
    _extract_binary_from_tar(archive_path, binary_name, destination)


def _extract_binary_from_tar(archive_path: Path, binary_name: str, destination: Path) -> None:
    with tarfile.open(str(archive_path), "r:*") as archive:
        member = _find_tar_binary_member(archive, binary_name)
        if member is None:
            raise IngestrNotFoundError("%s was not found in %s" % (binary_name, archive_path.name))
        source = archive.extractfile(member)
        if source is None:
            raise IngestrNotFoundError("%s could not be read from %s" % (binary_name, archive_path.name))
        with source:
            with destination.open("wb") as output:
                shutil.copyfileobj(source, output)


def _extract_binary_from_zip(archive_path: Path, binary_name: str, destination: Path) -> None:
    with zipfile.ZipFile(str(archive_path)) as archive:
        member = _find_zip_binary_member(archive, binary_name)
        if member is None:
            raise IngestrNotFoundError("%s was not found in %s" % (binary_name, archive_path.name))
        with archive.open(member) as source:
            with destination.open("wb") as output:
                shutil.copyfileobj(source, output)


def _find_tar_binary_member(archive: tarfile.TarFile, binary_name: str) -> Optional[tarfile.TarInfo]:
    for member in archive.getmembers():
        if member.isfile() and Path(member.name).name == binary_name:
            return member
    return None


def _find_zip_binary_member(archive: zipfile.ZipFile, binary_name: str) -> Optional[str]:
    for name in archive.namelist():
        if not name.endswith("/") and Path(name).name == binary_name:
            return name
    return None


def _release_asset_url(tag: str, archive_name: str) -> str:
    return "%s/%s/%s" % (
        _GITHUB_RELEASE_BASE_URL,
        urllib.parse.quote(tag, safe=""),
        urllib.parse.quote(archive_name, safe=""),
    )


def _release_archive_name(release: _ReleasePlatform) -> str:
    return "ingestr_%s_%s.%s" % (release.os_name, release.arch, release.archive_suffix)


def _release_platform() -> _ReleasePlatform:
    system = platform.system().lower()
    machine = platform.machine().lower()

    os_names = {
        "darwin": "Darwin",
        "linux": "Linux",
        "windows": "Windows",
    }
    arch_names = {
        "amd64": "x86_64",
        "x86_64": "x86_64",
        "x64": "x86_64",
        "arm64": "arm64",
        "aarch64": "arm64",
    }

    os_name = os_names.get(system)
    arch = arch_names.get(machine)
    if os_name is None or arch is None:
        raise IngestrNotFoundError("ingestr release binaries are not available for %s/%s" % (system, machine))
    if os_name == "Linux" and _linux_uses_musl():
        raise IngestrNotFoundError(
            "ingestr release binaries require glibc on Linux; build a musl-compatible binary and set %s"
            % _BINARY_PATH_ENV
        )
    if os_name == "Windows" and arch != "x86_64":
        raise IngestrNotFoundError("ingestr release binaries are not available for %s/%s" % (system, machine))

    return _ReleasePlatform(
        os_name=os_name,
        arch=arch,
        archive_suffix="zip" if os_name == "Windows" else "tar.gz",
        binary_name="ingestr.exe" if os_name == "Windows" else "ingestr",
    )


def _linux_uses_musl() -> bool:
    libc_name = platform.libc_ver()[0].lower()
    if "musl" in libc_name:
        return True

    for directory in (Path("/lib"), Path("/usr/lib")):
        try:
            if any(directory.glob("ld-musl-*.so.1")):
                return True
        except OSError:
            continue
    return False


def _release_tag() -> str:
    configured = os.environ.get(_BINARY_TAG_ENV)
    if configured:
        return configured

    package_version = _package_version()
    public_version = package_version.split("+", 1)[0]
    if not public_version or "dev" in public_version or public_version == "0":
        raise IngestrNotFoundError(
            "cannot infer an ingestr GitHub release tag from package version %r; set %s or %s"
            % (package_version, _BINARY_TAG_ENV, _BINARY_PATH_ENV)
        )
    if public_version.startswith("v"):
        return public_version
    return "v" + public_version


def _package_version() -> str:
    try:
        return version("ingestr")
    except PackageNotFoundError:
        return "0+unknown"


def _cache_root() -> Path:
    configured = os.environ.get(_BINARY_CACHE_DIR_ENV)
    if configured:
        return Path(os.path.expandvars(configured)).expanduser()

    if os.name == "nt":
        root = os.environ.get("LOCALAPPDATA")
        if root:
            return Path(root) / "ingestr"
        return Path.home() / "AppData" / "Local" / "ingestr"

    if sys.platform == "darwin":
        return Path.home() / "Library" / "Caches" / "ingestr"

    root = os.environ.get("XDG_CACHE_HOME")
    if root:
        return Path(root) / "ingestr"
    return Path.home() / ".cache" / "ingestr"


def _ensure_executable(path: Path) -> None:
    if os.name == "nt":
        return
    mode = stat.S_IMODE(path.stat().st_mode)
    executable_mode = mode | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH
    if executable_mode != mode:
        path.chmod(executable_mode)


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

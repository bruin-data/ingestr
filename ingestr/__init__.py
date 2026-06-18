from ._data import IngestSession, ingest
from ._runner import IngestrNotFoundError, binary_path, build_ingest_args, ingest as run_cli, main, run

cli = run_cli

try:
    from importlib.metadata import PackageNotFoundError, version
except ModuleNotFoundError:
    __version__ = "0+unknown"
else:
    try:
        __version__ = version("ingestr")
    except PackageNotFoundError:
        __version__ = "0+unknown"

__all__ = [
    "IngestrNotFoundError",
    "IngestSession",
    "__version__",
    "binary_path",
    "build_ingest_args",
    "cli",
    "ingest",
    "main",
    "run",
    "run_cli",
]

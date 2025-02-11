try:
    from ingestr.src import buildinfo  # type: ignore[import-not-found,attr-defined]

    __version__ = buildinfo.version.lstrip("v")
except ImportError:
    __version__ = "0.0.0-dev"

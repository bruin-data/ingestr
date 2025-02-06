try:
    from ingestr.src import buildinfo
    __version__ = buildinfo.version.lstrip("v")  # type: ignore
except ImportError:
    __version__ = "0.0.0-dev"

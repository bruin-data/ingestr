try:
    from ingestr.src import buildinfo
    __version__ = buildinfo.version.lstrip("v")
except ImportError:
    __version__ = "0.0.0-dev"

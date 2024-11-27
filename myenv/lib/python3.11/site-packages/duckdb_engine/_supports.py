import duckdb
from packaging.version import Version

duckdb_version = Version(duckdb.__version__)


has_uhugeint_support = duckdb_version >= Version("0.10.0")


def has_comment_support() -> bool:
    """
    See https://github.com/duckdb/duckdb/pull/10372
    """
    try:
        with duckdb.connect(":memory:") as con:
            con.execute("CREATE TABLE t (i INTEGER);")
            con.execute("COMMENT ON TABLE t IS 'test';")
    except duckdb.ParserException:
        return False
    return True

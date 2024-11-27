import duckdb
import pandas as pd
from pytest import mark, raises
from sqlalchemy import __version__, text
from sqlalchemy.engine import create_engine
from sqlalchemy.engine.base import Connection
from sqlalchemy.exc import ProgrammingError

df = pd.DataFrame([{"a": 1}])


@mark.skipif(not hasattr(Connection, "exec_driver_sql"), reason="Needs exec_driver_sql")
def test_register_driver(conn: Connection) -> None:
    conn.exec_driver_sql("register", ("test_df_driver", df))  # type: ignore[arg-type]
    conn.execute(text("select * from test_df_driver"))


def test_plain_register(conn: Connection) -> None:
    if __version__.startswith("1.3"):
        conn.execute(text("register"), {"name": "test_df", "df": df})
    else:
        conn.execute(text("register(:name, :df)"), {"name": "test_df", "df": df})
    conn.execute(text("select * from test_df"))


duckdb_version = duckdb.__version__


@mark.remote_data
@mark.skipif(
    "dev" in duckdb_version, reason="md extension not available for dev builds"
)
@mark.skipif(
    duckdb_version != "1.1.1", reason="md extension not available for this version"
)
def test_motherduck() -> None:
    engine = create_engine(
        "duckdb:///md:motherdb",
        connect_args={"config": {"motherduck_token": "motherduckdb_token"}},
    )

    with raises(
        ProgrammingError,
        match="Jwt is not in the form of Header.Payload.Signature with two dots and 3 sections",
    ):
        engine.connect()

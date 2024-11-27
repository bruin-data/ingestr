import logging
import os
import re
import zlib
from datetime import datetime, timedelta
from pathlib import Path
from typing import Any, Generic, Optional, TypeVar, cast

import duckdb
import sqlalchemy
from hypothesis import assume, given, settings
from hypothesis.strategies import text as text_strat
from packaging.version import Version
from pytest import LogCaptureFixture, fixture, importorskip, mark, raises
from sqlalchemy import (
    Column,
    DateTime,
    ForeignKey,
    Integer,
    Interval,
    MetaData,
    Sequence,
    String,
    Table,
    create_engine,
    func,
    inspect,
    select,
    text,
    types,
)
from sqlalchemy.dialects import registry  # type: ignore
from sqlalchemy.engine import Connection, Engine
from sqlalchemy.engine.reflection import Inspector
from sqlalchemy.exc import DBAPIError
from sqlalchemy.ext.declarative import declarative_base
from sqlalchemy.orm import Session, relationship, sessionmaker

from .. import Dialect, supports_attach, supports_user_agent
from .._supports import has_comment_support

try:
    # sqlalchemy 2
    from sqlalchemy.engine import ObjectKind  # type: ignore[attr-defined]
    from sqlalchemy.orm import Mapped
except ImportError:
    # sqlalchemy 1
    T = TypeVar("T")

    class Mapped(Generic[T]):  # type: ignore[no-redef]
        pass


@fixture
def engine() -> Engine:
    registry.register("duckdb", "duckdb_engine", "Dialect")

    eng = create_engine("duckdb:///:memory:")
    Base.metadata.create_all(eng)
    return eng


Base = declarative_base()


class CompressedString(types.TypeDecorator):
    """Custom Column Type"""

    impl = types.BLOB

    def process_bind_param(self, value: Optional[str], dialect: Any) -> Optional[bytes]:
        if value is None:
            return None
        return zlib.compress(value.encode("utf-8"), level=9)

    def process_result_value(self, value: bytes, dialect: Any) -> str:
        return zlib.decompress(value).decode("utf-8")


class TableWithBinary(Base):
    __tablename__ = "table_with_binary"

    id = Column(Integer(), Sequence("id_seq"), primary_key=True)

    text = Column(CompressedString())


class FakeModel(Base):
    __tablename__ = "fake"

    id = Column(Integer, Sequence("fakemodel_id_sequence"), primary_key=True)
    name = Column(String)

    owner: Mapped["Owner"] = relationship("Owner")


class Owner(Base):
    __tablename__ = "owner"
    id = Column(Integer, Sequence("owner_id"), primary_key=True)

    fake_id = Column(Integer, ForeignKey("fake.id"))
    owned: Mapped[FakeModel] = relationship(FakeModel, back_populates="owner")


class IntervalModel(Base):
    __tablename__ = "IntervalModel"

    id = Column(Integer, Sequence("IntervalModel_id_sequence"), primary_key=True)

    field = Column(Interval)


@fixture
def session(engine: Engine) -> Session:
    return sessionmaker(bind=engine)()


def test_basic(session: Session) -> None:
    session.add(FakeModel(name="Frank"))
    session.commit()

    frank = session.query(FakeModel).one()  # act

    assert frank.name == "Frank"


def test_foreign(session: Session) -> None:
    model = FakeModel(name="Walter")
    session.add(model)
    session.add(Owner(owned=model))
    session.commit()

    owner = session.query(Owner).one()  # act

    assert owner.owned.name == "Walter"


def test_disabled_server_side_cursors(engine: Engine) -> None:
    connection = engine.connect().execution_options(stream_results=True)

    session = sessionmaker(bind=connection)()

    session.add(FakeModel(name="Walter"))
    session.commit()

    assert list(session.query(FakeModel).yield_per(1))


@given(text_strat())
@settings(deadline=timedelta(seconds=1))
def test_simple_string(s: str) -> None:
    assume("\x00" not in s)
    eng = create_engine("duckdb:///:memory:")
    Base.metadata.create_all(eng)
    session = sessionmaker(bind=eng)()
    model = FakeModel(name=s)
    session.add(model)
    session.add(Owner(owned=model))
    session.commit()

    owner = session.query(Owner).one()  # act

    assert owner.owned.name == s


def test_get_tables(inspector: Inspector) -> None:
    for table_name in inspector.get_table_names():
        assert inspector.has_table(table_name)
    assert inspector.get_view_names() == []


@mark.skipif(
    supports_attach is False,
    reason="ATTACH is not supported for DuckDB version < 0.7.0",
)
def test_get_schema_names(inspector: Inspector, session: Session) -> None:
    # Using multi-line strings because of all the single and double quotes flying around...
    cmds = [
        """CREATE SCHEMA "quack quack" """,
        """CREATE SCHEMA "daffy duck"."you're "" despicable" """,
    ]
    for cmd in cmds:
        session.execute(text(cmd))
        session.commit()

    # Deliberately excluding pg_catalog schema (to align with Postgres)
    names = inspector.get_schema_names()
    if supports_attach:
        assert names == [
            '"daffy duck".information_schema',
            '"daffy duck".main',
            '"daffy duck"."quack quack"',
            '"daffy duck"."you\'re "" despicable"',
            "memory.information_schema",
            "memory.main",
            'memory."quack quack"',
            "system.information_schema",
            "system.main",
            "temp.information_schema",
            "temp.main",
        ]
    else:
        assert names == ["quack quack", "information_schema", "main", "temp"]


@mark.skipif(
    supports_attach is False,
    reason="ATTACH is not supported for DuckDB version < 0.7.0",
)
def test_get_table_names(inspector: Inspector, session: Session) -> None:
    # Using multi-line strings because of all the single and double quotes flying around...
    cmds = [
        """CREATE TABLE "daffy duck"."quack quack"."t2" (i INTEGER, j INTEGER);""",
        """CREATE TABLE "t3" (i INTEGER, j INTEGER);""",
        """CREATE SCHEMA "porky" """,
        """CREATE TABLE "porky"."t4" (i INTEGER, j INTEGER);""",
    ]
    for cmd in cmds:
        session.execute(text(cmd))
        session.commit()

    for schema, table_names in zip(
        ['"daffy duck"."quack quack"', "main", "porky"], [["t1", "t2"], ["t3"], ["t4"]]
    ):
        _table_names = inspector.get_table_names(schema=schema)
        assert set(_table_names).issuperset(set(table_names))
        for _table_name in _table_names:
            assert inspector.has_table(_table_name, schema)

    table_names_all = inspector.get_table_names()
    assert set(table_names_all).issuperset({"t1", "t2", "t3", "t4"})
    for table_name in table_names_all:
        assert inspector.has_table(table_name)


def test_get_views(conn: Connection, dialect: Dialect) -> None:
    views = dialect.get_view_names(conn)
    assert views == []

    conn.execute(text("create view test as select 1"))
    conn.execute(
        text("create schema scheme; create view scheme.schema_test as select 1")
    )

    views = dialect.get_view_names(conn)
    assert views == ["test"]

    views = dialect.get_view_names(conn, schema="scheme")
    assert views == ["schema_test"]

    views = dialect.get_view_names(conn, schema="memory.scheme")
    assert views == ["schema_test"]

    assert dialect.has_table(conn, table_name="test")
    assert dialect.has_table(conn, table_name="schema_test", schema="scheme")


@mark.skipif(os.uname().machine == "aarch64", reason="not supported on aarch64")
@mark.remote_data
def test_preload_extension() -> None:
    duckdb.connect().execute("INSTALL httpfs")
    engine = create_engine(
        "duckdb:///",
        connect_args={
            "preload_extensions": ["httpfs"],
            "config": {"s3_region": "ap-southeast-2", "s3_use_ssl": True},
        },
    )

    # check that we get an error indicating that the extension was loaded
    with engine.connect() as conn, raises(Exception, match="HTTP HEAD"):
        conn.execute(
            text("SELECT * FROM read_parquet('https://domain/path/to/file.parquet');")
        )


@fixture
def inspector(engine: Engine, session: Session) -> Inspector:
    cmds = [
        """CREATE TABLE test (id INTEGER);""" """ATTACH ':memory:' AS "daffy duck" """,
        """CREATE SCHEMA "daffy duck"."quack quack" """,
        """CREATE TABLE "daffy duck"."quack quack"."t1" (i INTEGER, j INTEGER);""",
    ]
    for cmd in cmds:
        session.execute(text(cmd))
        session.commit()

    meta = MetaData()
    Table("test", meta)

    return inspect(engine)


def test_get_columns(inspector: Inspector, session: Session) -> None:
    cols = inspector.get_columns("test", None)
    assert len(cols) == 1
    assert cols[0]["name"] == "id"
    assert inspector.has_table("t1", '"daffy duck"."quack quack"')
    cols1 = inspector.get_columns("t1", None)
    cols2 = inspector.get_columns("t1", '"daffy duck"."quack quack"')
    cols3 = inspector.get_columns("t1", "daffy duck.quack quack")
    assert len(cols1) == 2
    assert cols1[0]["name"] == "i"
    assert cols1[1]["name"] == "j"
    assert cols1[0]["name"] == cols2[0]["name"] == cols3[0]["name"]
    assert cols1[1]["name"] == cols2[1]["name"] == cols3[1]["name"]


def test_get_foreign_keys(inspector: Inspector) -> None:
    assert inspector.get_foreign_keys("test", None) == []
    assert inspector.get_foreign_keys("t1", '"daffy duck"."quack quack"') == []


@mark.skipif(
    Version(sqlalchemy.__version__) < Version("2.0.0"),
    reason="2-arg pg_getconstraintdef not yet supported in duckdb",
)
def test_get_check_constraints(inspector: Inspector) -> None:
    assert inspector.get_check_constraints("test", None) == []
    assert inspector.get_check_constraints("t1", '"daffy duck"."quack quack"') == []


def test_get_unique_constraints(inspector: Inspector) -> None:
    inspector.get_unique_constraints("test", None)
    inspector.get_unique_constraints("t1", '"daffy duck"."quack quack"')


def test_reflect(session: Session, engine: Engine) -> None:
    session.execute(text("create table test (id int);"))
    session.commit()

    meta = MetaData()
    meta.reflect(only=["test"], bind=engine)


def test_get_multi_columns(engine: Engine) -> None:
    importorskip("sqlalchemy", "2.0.0-rc1")
    with engine.connect() as conn:
        assert cast(Dialect, engine.dialect).get_multi_columns(
            connection=conn,
            schema=None,
            filter_names=set(),
            scope=None,
            kind=(ObjectKind.TABLE,),
        )


def test_commit(session: Session, engine: Engine) -> None:
    importorskip("sqlalchemy", "1.4.0")
    session.execute(text("commit;"))

    InteractiveShell = importorskip("IPython.core.interactiveshell").InteractiveShell

    shell = InteractiveShell()
    assert not shell.run_line_magic("load_ext", "sql")
    assert not shell.run_line_magic("sql", "duckdb:///:memory:")
    assert shell.run_line_magic("sql", "select 42;") == [(42,)]


def test_table_reflect(session: Session, engine: Engine) -> None:
    session.execute(text("create table test (id int);"))
    session.commit()

    meta = MetaData()
    user_table = Table("test", meta)
    insp = inspect(engine)

    reflect_table = (
        insp.reflecttable if hasattr(insp, "reflecttable") else insp.reflect_table
    )
    reflect_table(user_table, None)


def test_fetch_df_chunks() -> None:
    import duckdb

    duckdb.connect(":memory:").execute("select 1").fetch_df_chunk(1)


def test_fetchmany(engine: Engine) -> None:
    with engine.connect() as conn:
        res = conn.execute(text("select 1"))
        assert res.fetchmany(1) == [(1,)]


def test_description() -> None:
    import duckdb

    duckdb.connect("").description


def test_intervals(session: Session) -> None:
    session.add(IntervalModel(field=timedelta(days=1)))
    session.commit()

    owner = session.query(IntervalModel).one()  # act

    assert owner.field == timedelta(days=1)


def test_binary(session: Session) -> None:
    a = TableWithBinary(text="Hello World!")
    session.add(a)
    session.commit()

    b: TableWithBinary = session.query(TableWithBinary).one()
    assert b.text == "Hello World!"


@mark.skipif(
    not has_comment_support(), reason="comments not supported by duckdb version"
)
def test_comment_support(engine: Engine) -> None:
    "comments not yet supported by duckdb"

    class Notice(Base):
        __tablename__ = "tb_notice"
        __table_args__ = {"comment": "Notice table"}

        seqno = Column(
            Integer,
            Sequence("seqno_sequence"),
            primary_key=True,
            comment="Integer representing the sequence number",
        )
        title = Column(
            String(200),
            nullable=False,
            comment="Title of the notice, represented as a string",
        )

    Base.metadata.create_all(bind=engine)

    inspector = inspect(engine)

    assert inspector.get_table_comment("tb_notice")["text"] == "Notice table"

    columns = inspector.get_columns("tb_notice", None)
    assert columns[0]["comment"] == "Integer representing the sequence number"
    assert columns[1]["comment"] == "Title of the notice, represented as a string"


def test_rowcount() -> None:
    import duckdb

    assert duckdb.connect().rowcount == -1


def test_sessions(session: Session) -> None:
    c = IntervalModel(field=timedelta(seconds=5))
    session.add(c)
    session.commit()

    c2 = session.query(IntervalModel).get(1)
    assert c2
    c2.field = timedelta(days=5)
    session.flush()
    session.commit()


def test_inmemory() -> None:
    InteractiveShell = importorskip("IPython.core.interactiveshell").InteractiveShell

    shell = InteractiveShell()
    shell.run_cell("""import sqlalchemy as sa""")
    shell.run_cell("""eng = sa.create_engine("duckdb:///:memory:")""")
    shell.run_cell("""conn = eng.connect()""")
    shell.run_cell("""conn.execute(sa.text("CREATE TABLE t (x int)"))""")
    res = shell.run_cell("""conn.execute(sa.text("SHOW TABLES")).fetchall()""")

    assert res.result == [("t",)]


def test_config(tmp_path: Path) -> None:
    db_path = tmp_path / "test.db"

    db = duckdb.connect(str(db_path))
    db.execute("create table hello1 (i int)")
    db.close()

    eng = create_engine(
        f"duckdb:///{db_path}",
        connect_args={"read_only": True, "config": {"memory_limit": "500mb"}},
    )

    with raises(
        DBAPIError,
        match='Cannot execute statement of type "CREATE" (on database "test" which is attached )?in read-only mode!',
    ):
        with eng.connect() as conn:
            conn.execute(text("create table hello2 (i int)"))


def test_url_config() -> None:
    eng = create_engine("duckdb:///:memory:?worker_threads=123")

    with eng.connect() as conn:
        res = conn.execute(text("select current_setting('worker_threads')"))
        row = res.first()
        assert row is not None
        assert row[0] == 123


def test_url_config_and_dict_config() -> None:
    eng = create_engine(
        "duckdb:///:memory:?worker_threads=123",
        connect_args={"config": {"memory_limit": "500mb"}},
    )

    with eng.connect() as conn:
        res = conn.execute(
            text(
                "select current_setting('worker_threads'), current_setting('memory_limit')"
            )
        )
        row = res.first()
        assert row is not None
        worker_threads, memory_limit = row
        assert worker_threads == 123
        assert memory_limit in ("500.0MB", "476.8 MiB")


@mark.skipif(
    supports_user_agent is False,
    reason="custom_user_agent is not supported for DuckDB version < 0.9.2",
)
def test_user_agent() -> None:
    eng = create_engine("duckdb:///:memory:")

    with eng.connect() as conn:
        res = conn.execute(text("PRAGMA USER_AGENT"))
        row = res.first()
        assert row is not None
        assert re.match(r"duckdb/.*(.*) python duckdb_engine/.*(sqlalchemy/.*)", row[0])


@mark.skipif(
    supports_user_agent is False,
    reason="custom_user_agent is not supported for DuckDB version < 0.9.2",
)
def test_user_agent_with_custom_user_agent() -> None:
    eng = create_engine(
        "duckdb:///:memory:", connect_args={"config": {"custom_user_agent": "custom"}}
    )

    with eng.connect() as conn:
        res = conn.execute(text("PRAGMA USER_AGENT"))
        row = res.first()
        assert row is not None
        assert re.match(
            r"duckdb/.*(.*) python duckdb_engine/.*(sqlalchemy/.*) custom", row[0]
        )


def test_do_ping(tmp_path: Path, caplog: LogCaptureFixture) -> None:
    engine = create_engine(
        "duckdb:///" + str(tmp_path / "db"), pool_pre_ping=True, pool_size=1
    )

    logger = cast(logging.Logger, engine.pool.logger)  # type: ignore
    logger.setLevel(logging.DEBUG)

    with caplog.at_level(logging.DEBUG, logger=logger.name):
        engine.connect()  # create a connection in the pool
        assert (
            engine.connect() is not None
        )  # grab the "stale" connection, which will cause a ping

        assert any(
            "Pool pre-ping on connection" in message for message in caplog.messages
        )


def test_try_cast(engine: Engine) -> None:
    try_cast = importorskip("sqlalchemy", "2.0.14").try_cast

    with engine.connect() as conn:
        query = select(try_cast("2022-01-01", DateTime))
        assert conn.execute(query).one() == (datetime(2022, 1, 1),)

        query = select(try_cast("not a date", DateTime))
        assert conn.execute(query).one() == (None,)


def test_params(engine: Engine) -> None:
    s = text("SELECT :x")
    with engine.connect() as conn:
        assert ("m",) == conn.execute(s, {"x": "m"}).fetchone()


def test_361(engine: Engine) -> None:
    importorskip("sqlalchemy", "2.0.0")

    with engine.connect() as conn:
        conn.execute(text("create table test (dt date);"))
        conn.execute(text("insert into test values ('2022-01-01');"))

        metadata = MetaData()
        metadata.reflect(bind=conn)
        test = metadata.tables["test"]
        part = "year"
        date_part = func.date_part(part, test.c.dt)

        stmt = select(date_part).select_from(test).group_by(date_part)
        assert conn.execute(stmt).fetchall() == [(2022,)]


def test_close(engine: Engine) -> None:
    with engine.connect() as conn:
        res = conn.execute(text("select 1"))
        res.close()


def test_with_cache(tmp_path: Path) -> None:
    importorskip("duckdb", "1.0.0")
    tmp_db_path = str(tmp_path / "db_cached")
    engine1 = create_engine(f"duckdb:///{tmp_db_path}?threads=1")
    engine2 = create_engine(f"duckdb:///{tmp_db_path}?threads=1")
    with engine1.connect() as conn1:
        with engine2.connect() as conn2:
            res1 = conn1.execute(
                text("select value from duckdb_settings() where name = 'threads'")
            ).fetchall()
            res2 = conn2.execute(
                text("select value from duckdb_settings() where name = 'threads'")
            ).fetchall()
            assert res1 == res2
            # TODO: how do we validate that both connections point to the same database instance?
            assert res1[0][0] == "1"


def test_no_cache(tmp_path: Path) -> None:
    tmp_db_path = str(tmp_path / "db_no_cache")
    engine1 = create_engine(f"duckdb:///{tmp_db_path}?threads=1&user=1")
    engine2 = create_engine(f"duckdb:///{tmp_db_path}?threads=2&user=2")
    with engine1.connect() as conn1:
        with engine2.connect() as conn2:
            res1 = conn1.execute(
                text("select value from duckdb_settings() where name = 'threads'")
            ).fetchall()
            res2 = conn2.execute(
                text("select value from duckdb_settings() where name = 'threads'")
            ).fetchall()
            assert res1 != res2
            assert res1[0][0] == "1"
            assert res2[0][0] == "2"


def test_reflection(engine: Engine) -> None:
    metadata = MetaData()
    with engine.connect() as conn:
        conn.execute(text("CREATE TABLE tbl(col1 INTEGER)"))
    metadata.reflect(engine)

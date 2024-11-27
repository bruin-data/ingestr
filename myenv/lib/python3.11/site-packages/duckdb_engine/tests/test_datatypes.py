import decimal
import json
import warnings
from typing import Any, Dict, Type
from uuid import uuid4

from packaging.version import Version
from pytest import importorskip, mark
from snapshottest.module import SnapshotTest
from sqlalchemy import (
    Column,
    Integer,
    Interval,
    MetaData,
    Sequence,
    String,
    Table,
    inspect,
    schema,
    select,
    text,
)
from sqlalchemy.dialects.postgresql import UUID
from sqlalchemy.engine import Engine, create_engine
from sqlalchemy.ext.declarative import declarative_base
from sqlalchemy.orm import Session
from sqlalchemy.sql import sqltypes
from sqlalchemy.types import JSON

from .._supports import duckdb_version, has_uhugeint_support
from ..datatypes import Map, Struct, types


@mark.parametrize("coltype", types)
@mark.skipif(not has_uhugeint_support, reason="duckdb version too old")
def test_unsigned_integer_type(
    engine: Engine, session: Session, coltype: Type[Integer]
) -> None:
    Base = declarative_base()

    tname = "table"
    table = type(
        "Table",
        (Base,),
        {
            "__tablename__": tname,
            "id": Column(Integer, primary_key=True, default=0),
            "a": Column(coltype),
        },
    )
    Base.metadata.create_all(engine)

    has_table = (
        engine.has_table if hasattr(engine, "has_table") else inspect(engine).has_table
    )

    assert has_table(tname)

    session.add(table(a=1))
    session.commit()

    assert session.query(table).one()


@mark.remote_data()
def test_raw_json(engine: Engine) -> None:
    importorskip("duckdb", "0.9.3.dev4040")

    with engine.connect() as conn:
        assert conn.execute(text("load json"))

        assert conn.execute(text("select {'Hello': 'world'}::JSON")).fetchone() == (
            {"Hello": "world"},
        )


@mark.remote_data()
def test_custom_json_serializer() -> None:
    def default(o: Any) -> Any:
        if isinstance(o, decimal.Decimal):
            return {"__tag": "decimal", "value": str(o)}

    def object_hook(pairs: Dict[str, Any]) -> Any:
        if pairs.get("__tag", None) == "decimal":
            return decimal.Decimal(pairs["value"])
        else:
            return pairs

    engine = create_engine(
        "duckdb://",
        json_serializer=json.JSONEncoder(default=default).encode,
        json_deserializer=json.JSONDecoder(object_hook=object_hook).decode,
    )

    Base = declarative_base()

    class Entry(Base):
        __tablename__ = "test_json"
        id = Column(Integer, Sequence("id_seq"), primary_key=True)
        data = Column(JSON, nullable=False)

    Base.metadata.create_all(engine)

    with engine.connect() as conn:
        session = Session(bind=conn)

        data = {"hello": decimal.Decimal("42")}

        session.add(Entry(data=data))  # type: ignore[call-arg]
        session.commit()

        (res,) = session.execute(select(Entry)).one()

        assert res.data == data


def test_json(engine: Engine, session: Session) -> None:
    base = declarative_base()

    class Entry(base):
        __tablename__ = "test_json"

        id = Column(Integer, primary_key=True, default=0)
        meta = Column(JSON, nullable=False)

    base.metadata.create_all(bind=engine)

    session.add(Entry(meta={"hello": "world"}))  # type: ignore[call-arg]
    session.commit()

    result = session.query(Entry).one()

    assert result.meta == {"hello": "world"}


def test_uuid(engine: Engine, session: Session) -> None:
    importorskip("duckdb", "0.7.1")
    base = declarative_base()

    class Entry(base):
        __tablename__ = "test_uuid"

        id = Column(UUID, primary_key=True, default=0)

    base.metadata.create_all(bind=engine)

    ident = uuid4()

    session.add(Entry(id=ident))  # type: ignore[call-arg]
    session.commit()

    result = session.query(Entry).one()

    assert result.id == ident


def test_double_in_sqla_v2(engine: Engine) -> None:
    with engine.begin() as con:
        con.execute(text("CREATE TABLE t (x DOUBLE)"))
        con.execute(text("INSERT INTO t VALUES (1.0), (2.0), (3.0)"))

    md = MetaData()

    t = Table("t", md, autoload_with=engine)

    with engine.begin() as con:
        con.execute(t.select())


def test_all_types_reflection(engine: Engine) -> None:
    importorskip("sqlalchemy", "1.4.0")
    importorskip("duckdb", "0.5.1")

    with warnings.catch_warnings() as capture, engine.connect() as conn:
        conn.execute(text("create table t2 as select * from test_all_types()"))
        table = Table("t2", MetaData(), autoload_with=conn)
        for col in table.columns:
            name = col.name
            if name.endswith("_enum") and duckdb_version < Version("0.7.1"):
                continue
            if "array" in name or "struct" in name or "map" in name or "union" in name:
                assert col.type == sqltypes.NULLTYPE, name
            else:
                assert col.type != sqltypes.NULLTYPE, name
        assert not capture


def test_nested_types(engine: Engine, session: Session) -> None:
    importorskip("duckdb", "0.5.0")  # nested types require at least duckdb 0.5.0
    base = declarative_base()

    class Entry(base):
        __tablename__ = "test_struct"

        id = Column(Integer, primary_key=True, default=0)
        struct = Column(Struct(fields={"name": String}))
        map = Column(Map(String, Integer))
        # union = Column(Union(fields={"name": String, "age": Integer}))

    base.metadata.create_all(bind=engine)

    struct_data = {"name": "Edgar"}
    map_data = {"one": 1, "two": 2}

    session.add(Entry(struct=struct_data, map=map_data))  # type: ignore[call-arg]
    session.commit()

    result = session.query(Entry).one()

    assert result.struct == struct_data
    assert result.map == map_data


def test_double_nested_types(engine: Engine, session: Session) -> None:
    """Test for https://github.com/Mause/duckdb_engine/issues/1138"""
    importorskip("duckdb", "0.5.0")  # nested types require at least duckdb 0.5.0
    base = declarative_base()

    class Entry(base):
        __tablename__ = "test_struct"

        id = Column(Integer, primary_key=True, default=0)
        outer = Column(Struct({"inner": Struct({"val": Integer})}))

    base.metadata.create_all(bind=engine)

    outer = {"inner": {"val": 42}}

    session.add(Entry(outer=outer))  # type: ignore[call-arg]
    session.commit()

    result = session.query(Entry).one()

    assert result.outer == outer


def test_interval(engine: Engine, snapshot: SnapshotTest) -> None:
    test_table = Table("test_table", MetaData(), Column("duration", Interval))

    assert "duration INTERVAL" in str(schema.CreateTable(test_table).compile(engine))

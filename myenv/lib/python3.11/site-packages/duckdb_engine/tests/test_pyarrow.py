from pyarrow import RecordBatch, RecordBatchReader
from pyarrow import Table as ArrowTable
from sqlalchemy import MetaData, Table, create_engine, text


def test_fetch_arrow() -> None:
    engine = create_engine("duckdb:///:memory:")
    with engine.begin() as con:
        con.execute(text("CREATE TABLE tbl (label VARCHAR, value DOUBLE)"))
        con.execute(
            text(
                "INSERT INTO tbl VALUES ('xx',-1.0), ('ww',-4.5), ('zz',6.0), ('yy',2.5)"
            )
        )

    md = MetaData()
    t = Table("tbl", md, autoload_with=engine)
    stmt = t.select().where(t.c.value > -4.0).order_by(t.c.label)

    # rows
    with engine.begin() as con:
        res = con.execute(stmt).cursor.fetchall()
        assert res == [("xx", -1.0), ("yy", 2.5), ("zz", 6.0)]

    # arrow table
    with engine.begin() as con:
        res = con.execute(stmt).cursor.fetch_arrow_table()
        assert isinstance(res, ArrowTable)
        assert res == ArrowTable.from_pydict(
            {"label": ["xx", "yy", "zz"], "value": [-1.0, 2.5, 6.0]}
        )

    # arrow batches
    with engine.begin() as con:
        res = con.execute(stmt).cursor.fetch_record_batch()
        assert isinstance(res, RecordBatchReader)
        assert res.read_all() == ArrowTable.from_pydict(
            {"label": ["xx", "yy", "zz"], "value": [-1.0, 2.5, 6.0]}
        )
        res = con.execute(stmt).cursor.fetch_record_batch(rows_per_batch=2)
        assert res.read_next_batch() == RecordBatch.from_pydict(
            {"label": ["xx", "yy"], "value": [-1.0, 2.5]}
        )
        assert res.read_next_batch() == RecordBatch.from_pydict(
            {"label": ["zz"], "value": [6.0]}
        )

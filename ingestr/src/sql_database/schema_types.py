from typing import TYPE_CHECKING, Any, Optional, Sequence, Type

from dlt.common import logger
from dlt.common.configuration import with_config
from dlt.common.destination import DestinationCapabilitiesContext
from dlt.common.schema.typing import TColumnSchema, TTableSchemaColumns
from sqlalchemy import Column, Table
from sqlalchemy.engine import Row
from sqlalchemy.sql import Select, sqltypes
from typing_extensions import TypeAlias

# optionally create generics with any so they can be imported by dlt importer
if TYPE_CHECKING:
    SelectAny: TypeAlias = Select[Any]
    ColumnAny: TypeAlias = Column[Any]
    RowAny: TypeAlias = Row[Any]
else:
    SelectAny: TypeAlias = Type[Any]
    ColumnAny: TypeAlias = Type[Any]
    RowAny: TypeAlias = Type[Any]


def sqla_col_to_column_schema(
    sql_col: ColumnAny, add_precision: bool = False
) -> Optional[TColumnSchema]:
    """Infer dlt schema column type from an sqlalchemy type.

    If `add_precision` is set, precision and scale is inferred from that types that support it,
    such as numeric, varchar, int, bigint. Numeric (decimal) types have always precision added.
    """
    sql_t = sql_col.type
    col: TColumnSchema = {
        "name": sql_col.name,
        "data_type": None,  # set that later
        "nullable": sql_col.nullable,
    }

    if isinstance(sql_t, sqltypes.SmallInteger):
        col["data_type"] = "bigint"
        if add_precision:
            col["precision"] = 32
    elif isinstance(sql_t, sqltypes.Integer):
        col["data_type"] = "bigint"
    elif isinstance(sql_t, sqltypes.Numeric):
        # dlt column type depends on the data returned by the sql alchemy dialect
        # and not on the metadata reflected in the database. all Numeric types
        # that are returned as floats will assume "double" type
        # and returned as decimals will assume "decimal" type
        if sql_t.asdecimal is False:
            col["data_type"] = "double"
        else:
            col["data_type"] = "decimal"
            if sql_t.precision is not None:
                col["precision"] = sql_t.precision
                # must have a precision for any meaningful scale
                if sql_t.scale is not None:
                    col["scale"] = sql_t.scale
                elif sql_t.decimal_return_scale is not None:
                    col["scale"] = sql_t.decimal_return_scale
    elif isinstance(sql_t, sqltypes.String):
        col["data_type"] = "text"
        if add_precision and sql_t.length:
            col["precision"] = sql_t.length
    elif isinstance(sql_t, sqltypes._Binary):
        col["data_type"] = "binary"
        if add_precision and sql_t.length:
            col["precision"] = sql_t.length
    elif isinstance(sql_t, sqltypes.DateTime):
        col["data_type"] = "timestamp"
    elif isinstance(sql_t, sqltypes.Date):
        col["data_type"] = "date"
    elif isinstance(sql_t, sqltypes.Time):
        col["data_type"] = "time"
    elif isinstance(sql_t, sqltypes.JSON):
        col["data_type"] = "complex"
    elif isinstance(sql_t, sqltypes.Boolean):
        col["data_type"] = "bool"
    else:
        logger.warning(
            f"A column with name {sql_col.name} contains unknown data type {sql_t} which cannot be mapped to `dlt` data type. When using sqlalchemy backend such data will be passed to the normalizer. In case of `pyarrow` backend such data will be ignored. In case of other backends, the behavior is backend-specific."
        )
        col = None
    if col:
        return {key: value for key, value in col.items() if value is not None}  # type: ignore[return-value]
    return None


def table_to_columns(table: Table, add_precision: bool = False) -> TTableSchemaColumns:
    """Convert an sqlalchemy table to a dlt table schema.

    Adds precision to columns when `add_precision` is set.
    """
    return {
        col["name"]: col
        for col in (sqla_col_to_column_schema(c, add_precision) for c in table.columns)
        if col is not None
    }


@with_config
def columns_to_arrow(
    columns_schema: TTableSchemaColumns,
    caps: DestinationCapabilitiesContext = None,
    tz: str = "UTC",
) -> Any:
    """Converts `column_schema` to arrow schema using `caps` and `tz`. `caps` are injected from the container - which
    is always the case if run within the pipeline. This will generate arrow schema compatible with the destination.
    Otherwise generic capabilities are used
    """
    from dlt.common.destination.capabilities import DestinationCapabilitiesContext
    from dlt.common.libs.pyarrow import get_py_arrow_datatype
    from dlt.common.libs.pyarrow import pyarrow as pa

    return pa.schema(
        [
            pa.field(
                name,
                get_py_arrow_datatype(
                    schema_item,
                    caps or DestinationCapabilitiesContext.generic_capabilities(),
                    tz,
                ),
                nullable=schema_item.get("nullable", True),
            )
            for name, schema_item in columns_schema.items()
        ]
    )


def row_tuples_to_arrow(
    rows: Sequence[RowAny], columns: TTableSchemaColumns, tz: str
) -> Any:
    import numpy as np
    from dlt.common.libs.pyarrow import pyarrow as pa

    arrow_schema = columns_to_arrow(columns, tz=tz)

    try:
        from pandas._libs import lib

        pivoted_rows = lib.to_object_array_tuples(rows).T  # type: ignore[attr-defined]
    except ImportError:
        logger.info(
            "Pandas not installed, reverting to numpy.asarray to create a table which is slower"
        )
        pivoted_rows = np.asarray(rows, dtype="object", order="k").T  # type: ignore[call-overload]

    columnar = {
        col: dat.ravel()
        for col, dat in zip(columns, np.vsplit(pivoted_rows, len(columns)))
    }
    for idx in range(0, len(arrow_schema.names)):
        field = arrow_schema.field(idx)
        py_type = type(rows[0][idx])
        # cast double / float ndarrays to decimals if type mismatch, looks like decimals and floats are often mixed up in dialects
        if pa.types.is_decimal(field.type) and issubclass(py_type, (str, float)):
            logger.warning(
                f"Field {field.name} was reflected as decimal type, but rows contains {py_type.__name__}. Additional cast is required which may slow down arrow table generation."
            )
            float_array = pa.array(columnar[field.name], type=pa.float64())
            columnar[field.name] = float_array.cast(field.type, safe=False)
    return pa.Table.from_pydict(columnar, schema=arrow_schema)

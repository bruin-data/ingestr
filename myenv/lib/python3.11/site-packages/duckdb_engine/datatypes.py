"""
See https://duckdb.org/docs/sql/data_types/numeric for more information

Also
```sql
select * from duckdb_types where type_category = 'NUMERIC';
```
"""

import typing
from typing import Any, Callable, Dict, Optional, Type

import duckdb
from packaging.version import Version
from sqlalchemy import exc
from sqlalchemy.dialects.postgresql.base import PGIdentifierPreparer, PGTypeCompiler
from sqlalchemy.engine import Dialect
from sqlalchemy.ext.compiler import compiles
from sqlalchemy.sql import sqltypes, type_api
from sqlalchemy.sql.type_api import TypeEngine
from sqlalchemy.types import BigInteger, Integer, SmallInteger, String

# INTEGER	INT4, INT, SIGNED	-2147483648	2147483647
# SMALLINT	INT2, SHORT	-32768	32767
# BIGINT	INT8, LONG	-9223372036854775808	9223372036854775807
(BigInteger, SmallInteger)  # pure reexport

duckdb_version = duckdb.__version__

IS_GT_1 = Version(duckdb_version) > Version("1.0.0")


class UInt64(Integer):
    pass


class UInt32(Integer):
    pass


class UInt16(Integer):
    "AKA USMALLINT"


class UInt8(Integer):
    pass


class UTinyInteger(Integer):
    "AKA UInt1"

    name = "UTinyInt"
    # UTINYINT	-	0	255


class TinyInteger(Integer):
    "AKA Int1"

    name = "TinyInt"
    # TINYINT	INT1	-128	127


class USmallInteger(Integer):
    name = "usmallint"
    "AKA UInt2"
    # USMALLINT	-	0	65535
    min = 0
    max = 2**15


class UBigInteger(Integer):
    name = "UBigInt"
    min = 0
    max = 18446744073709551615


class HugeInteger(Integer):
    name = "HugeInt"
    # HUGEINT	 	-170141183460469231731687303715884105727*	170141183460469231731687303715884105727


class UHugeInteger(Integer):
    name = "UHugeInt"


class UInteger(Integer):
    # UINTEGER	-	0	4294967295
    pass


if IS_GT_1:

    class VarInt(Integer):
        pass


def compile_uint(element: Integer, compiler: PGTypeCompiler, **kw: Any) -> str:
    return getattr(element, "name", type(element).__name__)


types = [
    subclass
    for subclass in Integer.__subclasses__()
    if subclass.__module__ == UInt64.__module__
]
assert types


TV = typing.Union[Type[TypeEngine], TypeEngine]


class Struct(TypeEngine):
    """
    Represents a STRUCT type in DuckDB

    ```python
    from duckdb_engine.datatypes import Struct
    from sqlalchemy import Table, Column, String

    Table(
        'hello',
        Column('name', Struct({'first': String, 'last': String})
    )
    ```

    :param fields: only optional due to limitations with how much type information DuckDB returns to us in the description field
    """

    __visit_name__ = "struct"

    def __init__(self, fields: Optional[Dict[str, TV]] = None):
        self.fields = fields


class Map(TypeEngine):
    """
    Represents a MAP type in DuckDB

    ```python
    from duckdb_engine.datatypes import Map
    from sqlalchemy import Table, Column, String

    Table(
        'hello',
        Column('name', Map(String, String)
    )
    ```
    """

    __visit_name__ = "map"
    key_type: TV
    value_type: TV

    def __init__(self, key_type: TV, value_type: TV):
        self.key_type = key_type
        self.value_type = value_type

    def bind_processor(
        self, dialect: Dialect
    ) -> Optional[Callable[[Optional[dict]], Optional[dict]]]:
        return lambda value: (
            {"key": list(value), "value": list(value.values())} if value else None
        )

    def result_processor(
        self, dialect: Dialect, coltype: str
    ) -> Optional[Callable[[Optional[dict]], Optional[dict]]]:
        if IS_GT_1:
            return lambda value: value
        else:
            return (
                lambda value: dict(zip(value["key"], value["value"])) if value else {}
            )


class Union(TypeEngine):
    """
    Represents a UNION type in DuckDB

    ```python
    from duckdb_engine.datatypes import Union
    from sqlalchemy import Table, Column, String

    Table(
        'hello',
        Column('name', Union({"name": String, "age": String})
    )
    ```
    """

    __visit_name__ = "union"
    fields: Dict[str, TV]

    def __init__(self, fields: Dict[str, TV]):
        self.fields = fields


ISCHEMA_NAMES = {
    "hugeint": HugeInteger,
    "uhugeint": UHugeInteger,
    "tinyint": TinyInteger,
    "utinyint": UTinyInteger,
    "int8": BigInteger,
    "int4": Integer,
    "int2": SmallInteger,
    "timetz": sqltypes.TIME,
    "timestamptz": sqltypes.TIMESTAMP,
    "float4": sqltypes.FLOAT,
    "float8": sqltypes.FLOAT,
    "usmallint": USmallInteger,
    "uinteger": UInteger,
    "ubigint": UBigInteger,
    "timestamp_s": sqltypes.TIMESTAMP,
    "timestamp_ms": sqltypes.TIMESTAMP,
    "timestamp_ns": sqltypes.TIMESTAMP,
    "enum": sqltypes.Enum,
    "bool": sqltypes.BOOLEAN,
    "varchar": String,
}
if IS_GT_1:
    ISCHEMA_NAMES["varint"] = VarInt


def register_extension_types() -> None:
    for subclass in types:
        compiles(subclass, "duckdb")(compile_uint)


@compiles(Struct, "duckdb")  # type: ignore[misc]
def visit_struct(
    instance: Struct,
    compiler: PGTypeCompiler,
    identifier_preparer: PGIdentifierPreparer,
    **kw: Any,
) -> str:
    return "STRUCT" + struct_or_union(instance, compiler, identifier_preparer, **kw)


@compiles(Union, "duckdb")  # type: ignore[misc]
def visit_union(
    instance: Union,
    compiler: PGTypeCompiler,
    identifier_preparer: PGIdentifierPreparer,
    **kw: Any,
) -> str:
    return "UNION" + struct_or_union(instance, compiler, identifier_preparer, **kw)


def struct_or_union(
    instance: typing.Union[Union, Struct],
    compiler: PGTypeCompiler,
    identifier_preparer: PGIdentifierPreparer,
    **kw: Any,
) -> str:
    fields = instance.fields
    if fields is None:
        raise exc.CompileError(f"DuckDB {repr(instance)} type requires fields")
    return "({})".format(
        ", ".join(
            "{} {}".format(
                identifier_preparer.quote_identifier(key),
                process_type(
                    value, compiler, identifier_preparer=identifier_preparer, **kw
                ),
            )
            for key, value in fields.items()
        )
    )


def process_type(
    value: typing.Union[TypeEngine, Type[TypeEngine]],
    compiler: PGTypeCompiler,
    **kw: Any,
) -> str:
    return compiler.process(type_api.to_instance(value), **kw)


@compiles(Map, "duckdb")  # type: ignore[misc]
def visit_map(instance: Map, compiler: PGTypeCompiler, **kw: Any) -> str:
    return "MAP({}, {})".format(
        process_type(instance.key_type, compiler, **kw),
        process_type(instance.value_type, compiler, **kw),
    )

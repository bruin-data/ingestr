from typing import Any

from ... import cast as cast
from ... import Column as Column
from ... import MetaData as MetaData
from ... import Table as Table
from ... import util as util
from ...ext.compiler import compiles as compiles
from ...sql import expression as expression
from ...types import Boolean as Boolean
from ...types import Integer as Integer
from ...types import Numeric as Numeric
from ...types import String as String
from ...types import TypeDecorator as TypeDecorator
from ...types import Unicode as Unicode

ischema: Any

class CoerceUnicode(TypeDecorator):
    impl: Any = ...
    def process_bind_param(self, value: Any, dialect: Any): ...
    def bind_expression(self, bindvalue: Any): ...

class _cast_on_2005(expression.ColumnElement):
    bindvalue: Any = ...
    def __init__(self, bindvalue: Any) -> None: ...

schemata: Any
tables: Any
columns: Any
mssql_temp_table_columns: Any
constraints: Any
column_constraints: Any
key_constraints: Any
ref_constraints: Any
views: Any
computed_columns: Any
sequences: Any

class IdentitySqlVariant(TypeDecorator):
    impl: Any = ...
    def column_expression(self, colexpr: Any): ...

identity_columns: Any

# fmt: off
from typing import Any

from sqlalchemy import processors as processors
from sqlalchemy import types as sqltypes
from sqlalchemy.connectors.pyodbc import PyODBCConnector as PyODBCConnector
from sqlalchemy.dialects.sybase.base import SybaseDialect as SybaseDialect
from sqlalchemy.dialects.sybase.base import SybaseExecutionContext as SybaseExecutionContext
# fmt: on

class _SybNumeric_pyodbc(sqltypes.Numeric):
    def bind_processor(self, dialect: Any): ...

class SybaseExecutionContext_pyodbc(SybaseExecutionContext):
    def set_ddl_autocommit(self, connection: Any, value: Any) -> None: ...

class SybaseDialect_pyodbc(PyODBCConnector, SybaseDialect):
    execution_ctx_cls: Any = ...
    colspecs: Any = ...
    @classmethod
    def dbapi(cls): ...

dialect = SybaseDialect_pyodbc

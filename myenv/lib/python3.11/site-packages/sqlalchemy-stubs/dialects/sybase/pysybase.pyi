# fmt: off
from typing import Any
from typing import Optional

from sqlalchemy import processors as processors
from sqlalchemy import types as sqltypes
from sqlalchemy.dialects.sybase.base import SybaseDialect as SybaseDialect
from sqlalchemy.dialects.sybase.base import SybaseExecutionContext as SybaseExecutionContext
from sqlalchemy.dialects.sybase.base import SybaseSQLCompiler as SybaseSQLCompiler
# fmt: on

class _SybNumeric(sqltypes.Numeric):
    def result_processor(self, dialect: Any, type_: Any): ...

class SybaseExecutionContext_pysybase(SybaseExecutionContext):
    def set_ddl_autocommit(
        self, dbapi_connection: Any, value: Any
    ) -> None: ...
    def pre_exec(self) -> None: ...

class SybaseSQLCompiler_pysybase(SybaseSQLCompiler):
    def bindparam_string(self, name: Any, **kw: Any): ...

class SybaseDialect_pysybase(SybaseDialect):
    driver: str = ...
    execution_ctx_cls: Any = ...
    statement_compiler: Any = ...
    colspecs: Any = ...
    @classmethod
    def dbapi(cls): ...
    def create_connect_args(self, url: Any): ...
    def do_executemany(
        self,
        cursor: Any,
        statement: Any,
        parameters: Any,
        context: Optional[Any] = ...,
    ) -> None: ...
    def is_disconnect(self, e: Any, connection: Any, cursor: Any): ...

dialect = SybaseDialect_pysybase

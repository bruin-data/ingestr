# fmt: off
from typing import Any

from sqlalchemy.connectors.mxodbc import MxODBCConnector as MxODBCConnector
from sqlalchemy.dialects.sybase.base import SybaseDialect as SybaseDialect
from sqlalchemy.dialects.sybase.base import SybaseExecutionContext as SybaseExecutionContext
# fmt: on

class SybaseExecutionContext_mxodbc(SybaseExecutionContext): ...

class SybaseDialect_mxodbc(MxODBCConnector, SybaseDialect):
    execution_ctx_cls: Any = ...

dialect = SybaseDialect_mxodbc

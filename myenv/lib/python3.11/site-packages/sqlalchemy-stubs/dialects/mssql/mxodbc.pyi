from typing import Any
from typing import Optional

from .base import TypingMSDate as _MSDate
from .base import MSTime as _MSTime
from .base import MSDialect as MSDialect
from .base import VARBINARY as VARBINARY
from .pyodbc import TypingMSNumeric_pyodbc as _MSNumeric_pyodbc
from .pyodbc import MSExecutionContext_pyodbc as MSExecutionContext_pyodbc
from ...connectors.mxodbc import MxODBCConnector as MxODBCConnector

class _MSNumeric_mxodbc(_MSNumeric_pyodbc): ...

class _MSDate_mxodbc(_MSDate):
    def bind_processor(self, dialect: Any): ...

class _MSTime_mxodbc(_MSTime):
    def bind_processor(self, dialect: Any): ...

class _VARBINARY_mxodbc(VARBINARY):
    def bind_processor(self, dialect: Any): ...

class MSExecutionContext_mxodbc(MSExecutionContext_pyodbc): ...

class MSDialect_mxodbc(MxODBCConnector, MSDialect):
    execution_ctx_cls: Any = ...
    colspecs: Any = ...
    description_encoding: Any = ...
    def __init__(
        self, description_encoding: Optional[Any] = ..., **params: Any
    ) -> None: ...

dialect = MSDialect_mxodbc

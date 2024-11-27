from typing import Any
from typing import Callable
from typing import Dict
from typing import List
from typing import NoReturn
from typing import Optional
from typing import Type
from typing import Union

from .. import util
from .._typing import TypingExecuteOptions as _ExecuteOptions
from .._typing import TypingExecuteParams as _ExecuteParams
from ..engine.base import Connection as _LegacyConnection
from ..engine.base import Engine as _LegacyEngine
from ..engine.base import NestedTransaction
from ..engine.base import OptionEngineMixin
from ..engine.base import Transaction
from ..engine.create import TypingDebug as _Debug
from ..engine.create import TypingIsolationLevel as _IsolationLevel
from ..engine.create import TypingParamStyle as _ParamStyle
from ..engine.create import TypingResetOnReturn as _ResetOnReturn
from ..engine.url import URL
from ..pool import Pool

NO_OPTIONS: util.immutabledict[Any, Any] = ...

def create_engine(
    url: Union[str, URL],
    *,
    case_sensitive: bool = ...,
    connect_args: Dict[Any, Any] = ...,
    convert_unicode: bool = ...,
    creator: Callable[..., Any] = ...,
    echo: Union[bool, _Debug] = ...,
    echo_pool: Union[bool, _Debug] = ...,
    enable_from_linting: bool = ...,
    encoding: str = ...,
    execution_options: _ExecuteOptions = ...,
    hide_parameters: bool = ...,
    implicit_returning: bool = ...,
    isolation_level: _IsolationLevel = ...,
    json_deserializer: Callable[..., Any] = ...,
    json_serializer: Callable[..., Any] = ...,
    label_length: Optional[int] = ...,
    listeners: Any = ...,
    logging_name: str = ...,
    max_identifier_length: Optional[int] = ...,
    max_overflow: int = ...,
    module: Optional[Any] = ...,
    paramstyle: Optional[_ParamStyle] = ...,
    pool: Optional[Pool] = ...,
    poolclass: Optional[Type[Pool]] = ...,
    pool_logging_name: str = ...,
    pool_pre_ping: bool = ...,
    pool_size: int = ...,
    pool_recycle: int = ...,
    pool_reset_on_return: Optional[_ResetOnReturn] = ...,
    pool_timeout: float = ...,
    pool_use_lifo: bool = ...,
    plugins: List[str] = ...,
    query_cache_size: int = ...,
    **kw: Any,
) -> Engine: ...

class Connection(_LegacyConnection):
    def begin(self) -> Transaction: ...
    def begin_nested(self) -> NestedTransaction: ...
    def commit(self) -> None: ...
    def rollback(self) -> None: ...
    def close(self) -> None: ...
    def execute(  # type: ignore[override]
        self,
        statement: Any,
        parameters: Optional[_ExecuteParams] = ...,
        execution_options: Optional[_ExecuteOptions] = ...,
    ) -> Any: ...
    def scalar(  # type: ignore[override]
        self,
        statement: Any,
        parameters: Optional[_ExecuteParams] = ...,
        execution_options: Optional[_ExecuteOptions] = ...,
    ) -> Any: ...

class Engine(_LegacyEngine):
    def transaction(self, *arg: Any, **kw: Any) -> NoReturn: ...
    def run_callable(self, *arg: Any, **kw: Any) -> NoReturn: ...
    def execute(self, *arg: Any, **kw: Any) -> NoReturn: ...
    def scalar(self, *arg: Any, **kw: Any) -> NoReturn: ...
    def table_names(self, *arg: Any, **kw: Any) -> NoReturn: ...
    def has_table(self, *arg: Any, **kw: Any) -> NoReturn: ...
    class _trans_ctx:
        conn: Connection = ...
        def __init__(self, conn: Connection) -> None: ...
        transaction: Transaction = ...
        def __enter__(self) -> Connection: ...
        def __exit__(self, type_: Any, value: Any, traceback: Any) -> None: ...
    def begin(self) -> _trans_ctx: ...  # type: ignore[override]
    def connect(self) -> Connection: ...  # type: ignore[override]

class OptionEngine(OptionEngineMixin, Engine): ...

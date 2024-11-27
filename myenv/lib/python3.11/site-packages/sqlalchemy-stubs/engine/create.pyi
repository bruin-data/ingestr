from typing import Any
from typing import Callable
from typing import Dict
from typing import List
from typing import Optional
from typing import overload
from typing import Type
from typing import Union

from typing_extensions import Literal

from .base import Engine
from .url import URL
from .._typing import TypingExecuteOptions as _ExecuteOptions
from ..future import Engine as FutureEngine
from ..pool import Pool

TypingDebug = _Debug = Literal["debug"]
TypingIsolationLevel = _IsolationLevel = Literal[
    "SERIALIZABLE",
    "REPEATABLE READ",
    "READ COMMITTED",
    "READ UNCOMMITTED",
    "AUTOCOMMIT",
]
TypingParamStyle = _ParamStyle = Literal[
    "qmark", "numeric", "named", "format", "pyformat"
]
TypingResetOnReturn = _ResetOnReturn = Literal["rollback", "commit"]

@overload
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
    future: Literal[True],
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
    **kwargs: Any,
) -> FutureEngine: ...
@overload
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
    future: Literal[False] = ...,
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
    **kwargs: Any,
) -> Engine: ...
@overload
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
    future: Union[bool],
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
    **kwargs: Any,
) -> Union[Engine, FutureEngine]: ...
def engine_from_config(
    configuration: Any, prefix: str = ..., **kwargs: Any
) -> Engine: ...

from typing import Any
from typing import Callable
from typing import Collection
from typing import Generator
from typing import Iterable
from typing import Literal
from typing import Mapping
from typing import Optional
from typing import Protocol
from typing import Sequence
from typing import Type
from typing import TypeVar
from typing import Union

from .base import StartableContext
from .engine import AsyncConnection
from .engine import AsyncEngine
from .result import AsyncResult
from .result import AsyncScalarResult
from ..._typing import TypingExecuteOptions as _ExecuteOptions
from ..._typing import TypingExecuteParams as _ExecuteParams
from ...engine import Result
from ...engine import ScalarResult
from ...orm import Session
from ...orm.session import TypingBindArguments as _BindArguments
from ...orm.session import (
    TypingSessionClassMethodNoIoTypingCommon as _SessionClassMethodNoIoTypingCommon,
)
from ...orm.session import (
    TypingSessionInTransactionTypingCommon as _SessionInTransactionTypingCommon,
)
from ...orm.session import (
    TypingSessionNoIoTypingCommon as _SessionNoIoTypingCommon,
)
from ...orm.session import (
    TypingSharedSessionProtocol as _SharedSessionProtocol,
)
from ...sql import Executable

_T = TypeVar("_T")
_M = TypeVar("_M")
_TAsyncSession = TypeVar("_TAsyncSession", bound=AsyncSession)
_TAsyncSessionTransaction = TypeVar(
    "_TAsyncSessionTransaction", bound=AsyncSessionTransaction
)
_TAsyncSessionTransactionProtocol = TypeVar(
    "_TAsyncSessionTransactionProtocol",
    bound=_AsyncSessionTransactionProtocol,
)
_TAsyncSessionProtocol = TypeVar(
    "_TAsyncSessionProtocol", bound=_AsyncSessionProtocol
)

class _AsyncSessionTransactionProtocol(Protocol):
    @property
    def is_active(self) -> bool: ...
    async def commit(
        self,
    ) -> Optional[_AsyncSessionTransactionProtocol]: ...
    async def rollback(
        self,
    ) -> Optional[_AsyncSessionTransactionProtocol]: ...
    async def start(
        self: _TAsyncSessionTransactionProtocol,
    ) -> _TAsyncSessionTransactionProtocol: ...
    def __await__(
        self: _TAsyncSessionTransactionProtocol,
    ) -> Generator[Any, None, _TAsyncSessionTransactionProtocol]: ...
    async def __aenter__(
        self: _TAsyncSessionTransactionProtocol,
    ) -> _TAsyncSessionTransactionProtocol: ...
    async def __aexit__(
        self, type_: Any, value: Any, traceback: Any
    ) -> None: ...

class _AsyncSessionProtocol(
    _SharedSessionProtocol[Union[AsyncConnection, AsyncEngine]], Protocol
):
    async def __aenter__(
        self: _TAsyncSessionProtocol,
    ) -> _TAsyncSessionProtocol: ...
    async def __aexit__(
        self, type_: Any, value: Any, traceback: Any
    ) -> None: ...
    def begin(self) -> _AsyncSessionTransactionProtocol: ...
    def begin_nested(self) -> _AsyncSessionTransactionProtocol: ...
    async def rollback(self) -> None: ...
    async def commit(self) -> None: ...
    async def connection(self) -> Any: ...
    async def execute(
        self,
        statement: Executable,
        params: Optional[_ExecuteParams] = ...,
        execution_options: Optional[_ExecuteOptions] = ...,
        bind_arguments: Optional[_BindArguments] = ...,
        **kw: Any,
    ) -> Result: ...
    async def scalar(
        self,
        statement: Executable,
        params: Optional[_ExecuteParams] = ...,
        execution_options: Optional[_ExecuteOptions] = ...,
        bind_arguments: Optional[_BindArguments] = ...,
        **kw: Any,
    ) -> Any: ...
    async def close(self) -> None: ...
    async def refresh(
        self,
        instance: Any,
        attribute_names: Optional[Iterable[str]] = ...,
        with_for_update: Optional[
            Union[Literal[True], Mapping[str, Any]]
        ] = ...,
    ) -> None: ...
    async def get(
        self,
        entity: Type[_T],
        ident: Any,
        options: Optional[Sequence[Any]] = ...,
        populate_existing: bool = ...,
        with_for_update: Optional[
            Union[Literal[True], Mapping[str, Any]]
        ] = ...,
        identity_token: Optional[Any] = ...,
        execution_options: Optional[_ExecuteOptions] = ...,
    ) -> Optional[_T]: ...
    async def stream(
        self,
        statement: Any,
        params: Optional[_ExecuteParams] = ...,
        execution_options: Optional[_ExecuteOptions] = ...,
        bind_arguments: Optional[_BindArguments] = ...,
        **kw: Any,
    ) -> AsyncResult: ...
    async def scalars(
        self,
        statement: Executable,
        params: Optional[_ExecuteParams] = ...,
        execution_options: Optional[_ExecuteOptions] = ...,
    ) -> ScalarResult: ...
    async def stream_scalars(
        self,
        statement: Executable,
        params: Optional[_ExecuteParams] = ...,
        execution_options: Optional[_ExecuteOptions] = ...,
    ) -> AsyncScalarResult: ...
    async def delete(self, instance: Any) -> None: ...
    async def merge(
        self,
        instance: _T,
        load: bool = ...,
        options: Optional[Sequence[Any]] = ...,
    ) -> _T: ...
    async def flush(
        self, objects: Optional[Collection[Any]] = ...
    ) -> None: ...
    @classmethod
    async def close_all(cls) -> None: ...  # NOTE: Deprecated.

class _AsyncSessionTypingCommon(
    _SessionNoIoTypingCommon[Union[AsyncConnection, AsyncEngine]],
    _SessionClassMethodNoIoTypingCommon,
):
    bind: Any = ...
    def begin(self, **kw: Any) -> AsyncSessionTransaction: ...
    def begin_nested(self, **kw: Any) -> AsyncSessionTransaction: ...
    async def close(self) -> None: ...
    async def commit(self) -> None: ...
    async def connection(self, **kw: Any) -> AsyncConnection: ...
    async def delete(self, instance: Any) -> None: ...
    async def execute(
        self,
        statement: Executable,
        params: Optional[_ExecuteParams] = ...,
        execution_options: Optional[_ExecuteOptions] = ...,
        bind_arguments: Optional[Mapping[str, Any]] = ...,
        **kw: Any,
    ) -> Result: ...
    async def flush(self, objects: Optional[Any] = ...) -> None: ...
    async def get(
        self,
        entity: Type[_M],
        ident: Any,
        options: Optional[Sequence[Any]] = ...,
        populate_existing: bool = ...,
        with_for_update: Optional[Any] = ...,
        identity_token: Optional[Any] = ...,
        execution_options: Optional[_ExecuteOptions] = ...,
    ) -> Optional[_M]: ...
    async def merge(
        self,
        instance: _M,
        load: bool = ...,
        options: Optional[Sequence[Any]] = ...,
    ) -> _M: ...
    async def refresh(
        self,
        instance: Any,
        attribute_names: Optional[Any] = ...,
        with_for_update: Optional[Any] = ...,
    ) -> None: ...
    async def rollback(self) -> None: ...
    async def scalar(
        self,
        statement: Executable,
        params: Optional[_ExecuteParams] = ...,
        execution_options: Optional[_ExecuteOptions] = ...,
        bind_arguments: Optional[Mapping[str, Any]] = ...,
        **kw: Any,
    ) -> Any: ...
    async def scalars(
        self,
        statement: Executable,
        params: Optional[_ExecuteParams] = ...,
        execution_options: Optional[_ExecuteOptions] = ...,
    ) -> ScalarResult: ...
    async def stream_scalars(
        self,
        statement: Executable,
        params: Optional[_ExecuteParams] = ...,
        execution_options: Optional[_ExecuteOptions] = ...,
    ) -> AsyncScalarResult: ...
    @classmethod
    async def close_all(self) -> None: ...

class AsyncSession(
    _AsyncSessionTypingCommon,
    _SessionInTransactionTypingCommon,
):
    dispatch: Any = ...
    binds: Any = ...
    sync_session: Session = ...
    sync_session_class: Type[Session] = ...
    def __init__(
        self,
        bind: Optional[Union[AsyncConnection, AsyncEngine]] = ...,
        binds: Optional[
            Mapping[object, Union[AsyncConnection, AsyncEngine]]
        ] = ...,
        sync_session_class: Type[Session] = ...,
        **kw: Any,
    ) -> None: ...
    async def run_sync(
        self, fn: Callable[..., _M], *arg: Any, **kw: Any
    ) -> _M: ...
    async def stream(
        self,
        statement: Any,
        params: Optional[_ExecuteParams] = ...,
        execution_options: Optional[_ExecuteOptions] = ...,
        bind_arguments: Optional[Mapping[str, Any]] = ...,
        **kw: Any,
    ) -> AsyncResult: ...
    async def __aenter__(self: _TAsyncSession) -> _TAsyncSession: ...
    async def __aexit__(
        self, type_: Any, value: Any, traceback: Any
    ) -> None: ...
    def get_transaction(self) -> Optional[AsyncSessionTransaction]: ...
    def get_nested_transaction(self) -> Optional[AsyncSessionTransaction]: ...

class _AsyncSessionContextManager:
    async_session: AsyncSession = ...
    trans: AsyncSessionTransaction = ...
    def __init__(self, async_session: AsyncSession) -> None: ...
    async def __aenter__(self) -> AsyncSession: ...
    async def __aexit__(
        self, type_: Any, value: Any, traceback: Any
    ) -> None: ...

class AsyncSessionTransaction(StartableContext["AsyncSessionTransaction"]):
    session: AsyncSession = ...
    nested: bool = ...
    sync_transaction: Optional[Any] = ...
    def __init__(self, session: AsyncSession, nested: bool = ...) -> None: ...
    @property
    def is_active(self) -> bool: ...
    async def rollback(self) -> Optional[AsyncSessionTransaction]: ...
    async def commit(self) -> Optional[AsyncSessionTransaction]: ...
    async def start(
        self: _TAsyncSessionTransaction,
    ) -> _TAsyncSessionTransaction: ...
    async def __aexit__(
        self, type_: Any, value: Any, traceback: Any
    ) -> None: ...

def async_object_session(instance: Any) -> Optional[AsyncSession]: ...
def async_session(session: Session) -> Optional[AsyncSession]: ...

TypingAsyncSessionTypingCommon = _AsyncSessionTypingCommon
TypingAsyncSessionProtocol = _AsyncSessionProtocol

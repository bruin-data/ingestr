from typing import Any
from typing import Callable
from typing import Generic
from typing import Optional
from typing import TypeVar

from .session import TypingSessionClassMethods as _SessionClassMethods
from .session import TypingSessionTypingCommon as _SessionTypingCommon
from .session import Session
from ..util import ScopedRegistry

_T = TypeVar("_T")

class ScopedSessionMixin(Generic[_T]):
    def __call__(self, **kw: Any) -> _T: ...
    def configure(self, **kwargs: Any) -> None: ...

class scoped_session(
    _SessionTypingCommon, _SessionClassMethods, ScopedSessionMixin[Session]
):
    session_factory: Callable[..., Session] = ...
    registry: ScopedRegistry = ...
    def __init__(
        self,
        session_factory: Callable[..., Session],
        scopefunc: Optional[Callable[..., Any]] = ...,
    ) -> None: ...
    def remove(self) -> None: ...
    def query_property(self, query_cls: Optional[Any] = ...) -> Any: ...

ScopedSession = scoped_session

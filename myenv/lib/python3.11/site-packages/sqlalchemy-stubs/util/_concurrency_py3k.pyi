import sys

from .. import util as util

if sys.version_info >= (3, 0):
    import asyncio as asyncio  # noqa
    from typing import Any
    from typing import Callable
    from typing import Coroutine
    from typing import TypeVar

    _T = TypeVar("_T")
    _AAL = TypeVar("_AAL", bound=AsyncAdaptedLock)
    def await_only(awaitable: Coroutine[_T, Any, Any]) -> _T: ...
    def await_fallback(awaitable: Coroutine[_T, Any, Any]) -> _T: ...
    async def greenlet_spawn(
        fn: Callable[..., _T],
        *args: Any,
        _require_await: bool = ...,
        **kwargs: Any,
    ) -> _T: ...
    class AsyncAdaptedLock:
        @util.memoized_property
        def mutex(self) -> Any: ...
        def __init__(self) -> None: ...
        def __enter__(self: _AAL) -> _AAL: ...
        def __exit__(self, *arg: Any, **kw: Any) -> None: ...

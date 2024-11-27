import sys
from typing import Any
from typing import NoReturn

have_greenlet: bool

if sys.version_info >= (3, 0):
    from ._concurrency_py3k import AsyncAdaptedLock as AsyncAdaptedLock
    from ._concurrency_py3k import asyncio as asyncio
    from ._concurrency_py3k import await_fallback as await_fallback
    from ._concurrency_py3k import await_only as await_only
    from ._concurrency_py3k import greenlet_spawn as greenlet_spawn
else:
    asyncio: None
    def await_only(thing: Any) -> Any: ...
    def await_fallback(thing: Any) -> Any: ...
    def greenlet_spawn(fn: Any, *args: Any, **kw: Any) -> NoReturn: ...
    def AsyncAdaptedLock(*args: Any, **kw: Any) -> NoReturn: ...

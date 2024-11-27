from typing import Any
from typing import Callable
from typing import Optional
from typing import Type
from typing import TypeVar

from .langhelpers import inject_docstring_text as inject_docstring_text

SQLALCHEMY_WARN_20: bool

_C = TypeVar("_C", bound=Type[Any])
_F = TypeVar("_F", bound=Callable[..., Any])

def warn_deprecated(msg: Any, version: Any, stacklevel: int = ...) -> None: ...
def warn_deprecated_limited(
    msg: Any, args: Any, version: Any, stacklevel: int = ...
) -> None: ...
def warn_deprecated_20(msg: Any, stacklevel: int = ...) -> None: ...
def deprecated_cls(
    version: Any, message: Any, constructor: str = ...
) -> Callable[[_C], _C]: ...
def deprecated_20_cls(
    clsname: Any,
    alternative: Optional[Any] = ...,
    constructor: str = ...,
    becomes_legacy: bool = ...,
) -> Callable[[_C], _C]: ...
def deprecated(
    version: Any,
    message: Optional[Any] = ...,
    add_deprecation_to_docstring: bool = ...,
    warning: Optional[Any] = ...,
    enable_warnings: bool = ...,
) -> Callable[[_F], _F]: ...
def moved_20(message: Any, **kw: Any) -> Callable[[_F], _F]: ...
def deprecated_20(
    api_name: Any,
    alternative: Optional[Any] = ...,
    becomes_legacy: bool = ...,
    **kw: Any,
) -> Callable[[_F], _F]: ...
def deprecated_params(**specs: Any) -> Callable[[_F], _F]: ...

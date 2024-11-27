from typing import Any
from typing import TypeVar

from . import elements
from . import type_api
from ..util import langhelpers

_TE = TypeVar("_TE", bound=type_api.TypeEngine[Any])

REQUIRED: langhelpers.TypingSymbol

class _multiparam_column(elements.ColumnElement[_TE]):
    index: Any = ...
    key: str = ...
    original: elements.ColumnElement[_TE] = ...
    default: Any = ...
    type: _TE = ...  # type: ignore[assignment]
    def __init__(
        self, original: elements.ColumnElement[_TE], index: Any
    ) -> None: ...
    def compare(self, other: Any, **kw: Any) -> bool: ...
    def __eq__(self, other: Any) -> bool: ...  # type: ignore[override]

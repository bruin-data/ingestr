from typing import Any
from typing import Generic
from typing import Mapping
from typing import overload
from typing import Sequence
from typing import Type
from typing import TypeVar
from typing import Union

_T = TypeVar("_T")

class _TypeToInstance(Generic[_T]):
    @overload
    def __get__(self, instance: None, owner: Any) -> Type[_T]: ...
    @overload
    def __get__(self, instance: object, owner: Any) -> _T: ...
    @overload
    def __set__(self, instance: None, value: Type[_T]) -> None: ...
    @overload
    def __set__(self, instance: object, value: _T) -> None: ...

_ExecuteParams = Union[Mapping[Any, Any], Sequence[Mapping[Any, Any]]]
_ExecuteOptions = Mapping[Any, Any]

TypingExecuteOptions = _ExecuteOptions
TypingExecuteParams = _ExecuteParams
TypingTypeToInstance = _TypeToInstance

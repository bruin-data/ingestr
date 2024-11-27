import decimal
from typing import Any
from typing import Generic
from typing import overload
from typing import Type
from typing import TypeVar
from typing import Union

_T = TypeVar("_T")

class registry:
    def __init__(self) -> None: ...
    def mapped(self, cls: type) -> type: ...

class TypeEngine(Generic[_T]):
    @property
    def python_type(self) -> Type[_T]: ...

class Integer(TypeEngine[int]):
    @property
    def python_type(self) -> Type[int]: ...

class String(TypeEngine[str]):
    @property
    def python_type(self) -> Type[str]: ...

class Numeric(TypeEngine[Union[decimal.Decimal, float]]):
    @property
    def python_type(self) -> Type[Union[decimal.Decimal, float]]: ...

class Float(Numeric):
    @property
    def python_type(self) -> Type[Union[decimal.Decimal, float]]: ...

class Column:
    type: Any = ...
    def __init__(self, type_: TypeEngine[_T]) -> None: ...

class relationship:
    type: Any = ...
    def __init__(self, type_: TypeEngine[_T]) -> None: ...

class Mapped:
    @overload
    def __get__(self, instance: None, owner: Any) -> Mapped[_T]: ...
    @overload
    def __get__(self, instance: object, owner: Any) -> Union[_T, None]: ...
    # TODO: this is a hack to work with the plugin for now, needs adjustment
    @classmethod
    def _empty_constructor(cls, _T) -> "Mapped[_T]": ...
    @property
    def property(self) -> object: ...
    # so that it can work with the "constructor", or none
    # def __init__(self, type: _T) -> None: ...

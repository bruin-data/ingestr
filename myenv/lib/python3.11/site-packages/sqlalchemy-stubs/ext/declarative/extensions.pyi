from typing import Any
from typing import Dict
from typing import Type

from ... import MetaData

def instrument_declarative(
    cls: Type[Any], cls_registry: Dict[str, Type[Any]], metadata: MetaData
) -> Any: ...

class ConcreteBase:
    @classmethod
    def __declare_first__(cls) -> None: ...

class AbstractConcreteBase(ConcreteBase):
    __no_table__: bool = ...
    @classmethod
    def __declare_first__(cls) -> None: ...

class DeferredReflection:
    @classmethod
    def prepare(cls, engine: Any) -> None: ...

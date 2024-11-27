from typing import Any
from typing import Callable
from typing import FrozenSet
from typing import Generic
from typing import MutableMapping
from typing import Optional
from typing import Tuple
from typing import Type
from typing import TypeVar
from typing import Union

from . import attributes as attributes
from . import clsregistry as clsregistry
from . import instrumentation as instrumentation
from . import interfaces as interfaces
from .mapper import Mapper as Mapper
from .. import inspection as inspection
from .. import util as util
from ..sql import ColumnElement
from ..sql import FromClause
from ..sql.schema import MetaData as MetaData
from ..util import hybridmethod as hybridmethod
from ..util import hybridproperty as hybridproperty

_T = TypeVar("_T")

def has_inherited_table(cls: type) -> bool: ...
def synonym_for(name: Any, map_column: bool = ...): ...

class declared_attr(interfaces._MappedAttribute, property, Generic[_T]):
    __doc__: Any = ...
    def __init__(self, fget: Any, cascading: bool = ...) -> None: ...
    def __get__(
        desc: Any, self: Any, cls: Any
    ) -> Union[interfaces.MapperProperty, ColumnElement]: ...
    def cascading(cls): ...

class _stateful_declared_attr(declared_attr):
    kw: Any = ...
    def __init__(self, **kw: Any) -> None: ...
    def __call__(self, fn: Any): ...

def declarative_mixin(cls: Type[_T]) -> Type[_T]: ...
def declarative_base(
    bind: Optional[
        Any
    ] = ...,  # NOTE: Deprecated in 1.4, to be removed in 2.0.
    metadata: Optional[MetaData] = ...,
    mapper: Optional[Callable[..., Mapper]] = ...,
    cls: Union[type, Tuple[type, ...]] = ...,
    name: str = ...,
    constructor: Optional[Callable[..., None]] = ...,
    class_registry: Optional[MutableMapping[Any, Any]] = ...,
    metaclass: type = ...,
) -> type: ...

class registry:
    metadata: MetaData
    constructor: Callable[..., None]
    def __init__(
        self,
        metadata: Optional[MetaData] = ...,
        class_registry: Optional[MutableMapping[Any, Any]] = ...,
        constructor: Callable[..., None] = ...,
        _bind: Optional[Any] = ...,
    ) -> None: ...
    @property
    def mappers(self) -> FrozenSet[Mapper]: ...
    def configure(self, cascade: bool = ...) -> None: ...
    def dispose(self, cascade: bool = ...) -> None: ...
    def generate_base(
        self,
        mapper: Optional[Callable[..., Mapper]] = ...,
        cls: Union[type, Tuple[type, ...]] = ...,
        name: str = ...,
        metaclass: type = ...,
    ) -> type: ...
    def mapped(self, cls: Type[_T]) -> Type[_T]: ...
    def as_declarative_base(
        self, **kw: Any
    ) -> Callable[[Type[_T]], Type[_T]]: ...
    def map_declaratively(self, cls: type) -> Mapper: ...
    def map_imperatively(
        self,
        class_: type,
        local_table: Optional[FromClause] = ...,
        **kw: Any,
    ) -> Mapper: ...

_registry = registry

class DeclarativeMeta(type):
    def __init__(
        cls, classname: Any, bases: Any, dict_: Any, **kw: Any
    ) -> None: ...
    def __setattr__(cls, key: Any, value: Any) -> None: ...
    def __delattr__(cls, key: Any) -> None: ...
    metadata: MetaData
    registry: _registry  # Avoid circural reference

def as_declarative(**kw: Any) -> Callable[[Type[_T]], Type[_T]]: ...

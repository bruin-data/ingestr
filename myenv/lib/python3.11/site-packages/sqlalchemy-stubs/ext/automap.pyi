from typing import Any
from typing import Optional

from .. import util as util
from ..orm import backref as backref
from ..orm import interfaces as interfaces
from ..orm import relationship as relationship
from ..schema import ForeignKeyConstraint as ForeignKeyConstraint
from ..sql import and_ as and_

def classname_for_table(base: Any, tablename: Any, table: Any): ...
def name_for_scalar_relationship(
    base: Any, local_cls: Any, referred_cls: Any, constraint: Any
): ...
def name_for_collection_relationship(
    base: Any, local_cls: Any, referred_cls: Any, constraint: Any
): ...
def generate_relationship(
    base: Any,
    direction: Any,
    return_fn: Any,
    attrname: Any,
    local_cls: Any,
    referred_cls: Any,
    **kw: Any,
): ...

class AutomapBase:
    __abstract__: bool = ...
    classes: Any = ...
    @classmethod
    def prepare(
        cls,
        autoload_with: Optional[Any] = ...,
        engine: Optional[Any] = ...,
        reflect: bool = ...,
        schema: Optional[Any] = ...,
        classname_for_table: Optional[Any] = ...,
        collection_class: Optional[Any] = ...,
        name_for_scalar_relationship: Optional[Any] = ...,
        name_for_collection_relationship: Optional[Any] = ...,
        generate_relationship: Optional[Any] = ...,
    ) -> None: ...

def automap_base(declarative_base: Optional[Any] = ..., **kw: Any): ...

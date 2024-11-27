from typing import Any

from . import exc as exc
from .. import inspection as inspection
from .. import util as util

PASSIVE_NO_RESULT: Any
PASSIVE_CLASS_MISMATCH: Any
ATTR_WAS_SET: Any
ATTR_EMPTY: Any
NO_VALUE: Any
NEVER_SET: Any
NO_CHANGE: Any
CALLABLES_OK: Any
SQL_OK: Any
RELATED_OBJECT_OK: Any
INIT_OK: Any
NON_PERSISTENT_OK: Any
LOAD_AGAINST_COMMITTED: Any
NO_AUTOFLUSH: Any
NO_RAISE: Any
PASSIVE_OFF: Any
PASSIVE_RETURN_NO_VALUE: Any
PASSIVE_NO_INITIALIZE: Any
PASSIVE_NO_FETCH: Any
PASSIVE_NO_FETCH_RELATED: Any
PASSIVE_ONLY_PERSISTENT: Any
DEFAULT_MANAGER_ATTR: str
DEFAULT_STATE_ATTR: str
EXT_CONTINUE: Any
EXT_STOP: Any
EXT_SKIP: Any
ONETOMANY: Any
MANYTOONE: Any
MANYTOMANY: Any
NOT_EXTENSION: Any

def manager_of_class(cls): ...

instance_state: Any
instance_dict: Any

def instance_str(instance: Any): ...
def state_str(state: Any): ...
def state_class_str(state: Any): ...
def attribute_str(instance: Any, attribute: Any): ...
def state_attribute_str(state: Any, attribute: Any): ...
def object_mapper(instance: Any): ...
def object_state(instance: Any): ...
def class_mapper(class_: Any, configure: bool = ...): ...

class InspectionAttr:
    is_selectable: bool = ...
    is_aliased_class: bool = ...
    is_instance: bool = ...
    is_mapper: bool = ...
    is_bundle: bool = ...
    is_property: bool = ...
    is_attribute: bool = ...
    is_clause_element: bool = ...
    extension_type: Any = ...

class InspectionAttrInfo(InspectionAttr):
    @util.memoized_property
    def info(self): ...

class _MappedAttribute: ...

TypingMappedAttribute = _MappedAttribute

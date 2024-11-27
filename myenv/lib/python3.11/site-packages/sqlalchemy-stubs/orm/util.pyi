# fmt: off
from typing import Any
from typing import Optional

from . import attributes as attributes
from .base import attribute_str as attribute_str
from .base import class_mapper as class_mapper
from .base import InspectionAttr as InspectionAttr
from .base import instance_str as instance_str
from .base import object_mapper as object_mapper
from .base import object_state as object_state
from .base import state_attribute_str as state_attribute_str
from .base import state_class_str as state_class_str
from .base import state_str as state_str
from .interfaces import CriteriaOption as CriteriaOption
from .interfaces import MapperProperty as MapperProperty
from .interfaces import ORMColumnsClauseRole as ORMColumnsClauseRole
from .interfaces import ORMEntityColumnsClauseRole as ORMEntityColumnsClauseRole
from .interfaces import ORMFromClauseRole as ORMFromClauseRole
from .interfaces import PropComparator as PropComparator
from .path_registry import PathRegistry as PathRegistry
from .. import event as event
from .. import inspection as inspection
from .. import sql as sql
from .. import util as util
from ..engine.result import result_tuple as result_tuple
from ..sql import base as sql_base
from ..sql import coercions as coercions
from ..sql import expression as expression
from ..sql import lambdas as lambdas
from ..sql import roles as roles
from ..sql import traversals as traversals
from ..sql import util as sql_util
from ..sql import visitors as visitors
from ..sql.annotation import SupportsCloneAnnotations as SupportsCloneAnnotations
from ..sql.base import ColumnCollection as ColumnCollection
# fmt: on

all_cascades: Any

class CascadeOptions(frozenset):
    save_update: Any = ...
    delete: Any = ...
    refresh_expire: Any = ...
    merge: Any = ...
    expunge: Any = ...
    delete_orphan: Any = ...
    def __new__(cls, value_list: Any): ...
    @classmethod
    def from_string(cls, arg: Any): ...

def polymorphic_union(
    table_map: Any,
    typecolname: Any,
    aliasname: str = ...,
    cast_nulls: bool = ...,
): ...
def identity_key(*args: Any, **kwargs: Any): ...

class ORMAdapter(sql_util.ColumnAdapter):
    mapper: Any = ...
    aliased_class: Any = ...
    def __init__(
        self,
        entity: Any,
        equivalents: Optional[Any] = ...,
        adapt_required: bool = ...,
        allow_label_resolve: bool = ...,
        anonymize_labels: bool = ...,
    ) -> None: ...

class AliasedClass:
    __name__: Any = ...
    def __init__(
        self,
        mapped_class_or_ac: Any,
        alias: Optional[Any] = ...,
        name: Optional[Any] = ...,
        flat: bool = ...,
        adapt_on_names: bool = ...,
        with_polymorphic_mappers: Any = ...,
        with_polymorphic_discriminator: Optional[Any] = ...,
        base_alias: Optional[Any] = ...,
        use_mapper_path: bool = ...,
        represents_outer_join: bool = ...,
    ) -> None: ...
    def __getattr__(self, key: Any): ...

class AliasedInsp(
    ORMEntityColumnsClauseRole,
    ORMFromClauseRole,
    traversals.MemoizedHasCacheKey,
    InspectionAttr,
):
    mapper: Any = ...
    selectable: Any = ...
    name: Any = ...
    polymorphic_on: Any = ...
    represents_outer_join: Any = ...
    with_polymorphic_mappers: Any = ...
    def __init__(
        self,
        entity: Any,
        inspected: Any,
        selectable: Any,
        name: Any,
        with_polymorphic_mappers: Any,
        polymorphic_on: Any,
        _base_alias: Any,
        _use_mapper_path: Any,
        adapt_on_names: Any,
        represents_outer_join: Any,
    ) -> None: ...
    @property
    def entity(self): ...
    is_aliased_class: bool = ...
    def __clause_element__(self): ...
    @property
    def entity_namespace(self): ...
    @property
    def class_(self): ...

class LoaderCriteriaOption(CriteriaOption):
    root_entity: Any = ...
    entity: Any = ...
    deferred_where_criteria: bool = ...
    where_criteria: Any = ...
    include_aliases: Any = ...
    propagate_to_loaders: Any = ...
    def __init__(
        self,
        entity_or_base: Any,
        where_criteria: Any,
        loader_only: bool = ...,
        include_aliases: bool = ...,
        propagate_to_loaders: bool = ...,
        track_closure_variables: bool = ...,
    ) -> None: ...
    def process_compile_state(self, compile_state: Any) -> None: ...
    def get_global_criteria(self, attributes: Any) -> None: ...

def aliased(
    element: Any,
    alias: Optional[Any] = ...,
    name: Optional[Any] = ...,
    flat: bool = ...,
    adapt_on_names: bool = ...,
): ...
def with_polymorphic(
    base: Any,
    classes: Any,
    selectable: bool = ...,
    flat: bool = ...,
    polymorphic_on: Optional[Any] = ...,
    aliased: bool = ...,
    innerjoin: bool = ...,
    _use_mapper_path: bool = ...,
    _existing_alias: Optional[Any] = ...,
): ...

class Bundle(ORMColumnsClauseRole, SupportsCloneAnnotations, InspectionAttr):
    single_entity: bool = ...
    is_clause_element: bool = ...
    is_mapper: bool = ...
    is_aliased_class: bool = ...
    is_bundle: bool = ...
    name: Any = ...
    exprs: Any = ...
    c: Any = ...
    def __init__(self, name: Any, *exprs: Any, **kw: Any) -> None: ...
    @property
    def mapper(self): ...
    @property
    def entity(self): ...
    @property
    def entity_namespace(self): ...
    columns: Any = ...
    def __clause_element__(self): ...
    @property
    def clauses(self): ...
    def label(self, name: Any): ...
    def create_row_processor(self, query: Any, procs: Any, labels: Any): ...

class _ORMJoin(expression.Join):
    __visit_name__: Any = ...
    inherit_cache: bool = ...
    onclause: Any = ...
    def __init__(
        self,
        left: Any,
        right: Any,
        onclause: Optional[Any] = ...,
        isouter: bool = ...,
        full: bool = ...,
        _left_memo: Optional[Any] = ...,
        _right_memo: Optional[Any] = ...,
        _extra_criteria: Any = ...,
    ) -> None: ...
    def join(
        self,
        right: Any,
        onclause: Optional[Any] = ...,
        isouter: bool = ...,
        full: bool = ...,
        join_to_left: Optional[Any] = ...,
    ): ...
    def outerjoin(
        self,
        right: Any,
        onclause: Optional[Any] = ...,
        full: bool = ...,
        join_to_left: Optional[Any] = ...,
    ): ...

def join(
    left: Any,
    right: Any,
    onclause: Optional[Any] = ...,
    isouter: bool = ...,
    full: bool = ...,
    join_to_left: Optional[Any] = ...,
): ...
def outerjoin(
    left: Any,
    right: Any,
    onclause: Optional[Any] = ...,
    full: bool = ...,
    join_to_left: Optional[Any] = ...,
): ...
def with_parent(
    instance: Any, prop: Any, from_entity: Optional[Any] = ...
): ...
def has_identity(object_: Any): ...
def was_deleted(object_: Any): ...
def randomize_unitofwork() -> None: ...

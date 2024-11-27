from typing import AbstractSet
from typing import Any
from typing import Callable
from typing import MutableMapping
from typing import Optional
from typing import Sequence
from typing import Tuple
from typing import Type
from typing import TypeVar
from typing import Union

from typing_extensions import Literal

from . import attributes as attributes
from . import TypingBackrefResult as _BackrefResult
from .base import state_str as state_str
from .interfaces import MANYTOMANY as MANYTOMANY
from .interfaces import MANYTOONE as MANYTOONE
from .interfaces import ONETOMANY as ONETOMANY
from .interfaces import PropComparator as PropComparator
from .interfaces import StrategizedProperty as StrategizedProperty
from .mapper import Mapper
from .util import AliasedInsp as AliasedInsp
from .util import CascadeOptions as CascadeOptions
from .. import log as log
from .. import schema as schema
from .. import sql as sql
from .. import util as util
from ..inspection import inspect as inspect
from ..sql import coercions as coercions
from ..sql import elements as elements
from ..sql import expression as expression
from ..sql import operators as operators
from ..sql import roles as roles
from ..sql import visitors as visitors
from ..sql.util import adapt_criterion_to_null as adapt_criterion_to_null
from ..sql.util import ClauseAdapter as ClauseAdapter
from ..sql.util import join_condition as join_condition
from ..sql.util import selectables_overlap as selectables_overlap
from ..sql.util import visit_binary_product as visit_binary_product

def remote(expr: Any): ...
def foreign(expr: Any): ...

_T = TypeVar("_T")

_OrderByArgument = Union[
    Literal[False],
    str,
    roles.OrderByRole,
    Sequence[roles.OrderByRole],
    Callable[
        [],
        Union[
            roles.OrderByRole,
            Sequence[roles.OrderByRole],
        ],
    ],
]

class RelationshipProperty(StrategizedProperty[_T]):
    strategy_wildcard_key: str
    inherit_cache: bool
    uselist: Optional[bool]
    argument: Any
    secondary: Any
    primaryjoin: Any
    secondaryjoin: Any
    post_update: bool
    direction: Any
    viewonly: Any
    sync_backref: Any
    lazy: str
    single_parent: Any
    collection_class: Optional[Union[Type[Any], Callable[[], Any]]]
    passive_deletes: Union[bool, Literal["all"]]
    cascade_backrefs: bool
    passive_updates: bool
    remote_side: Any
    enable_typechecks: bool  # NOTE: not documented
    query_class: Any
    innerjoin: Union[bool, str]
    distinct_target_key: Optional[bool]
    doc: Optional[str]
    active_history: bool
    join_depth: Optional[int]
    omit_join: Optional[Literal[False]]
    local_remote_pairs: Any  # NOTE: not documented
    bake_queries: bool
    load_on_pending: bool
    comparator_factory: Any
    comparator: Any
    info: MutableMapping[Any, Any]
    strategy_key: Tuple[Tuple[str, str]]  # NOTE: not documented
    order_by: Any
    back_populates: Union[None, str]
    backref: Union[None, str, _BackrefResult]
    def __init__(
        self,
        argument: Any,
        secondary: Optional[Any] = ...,
        primaryjoin: Optional[Any] = ...,
        secondaryjoin: Optional[Any] = ...,
        foreign_keys: Optional[Any] = ...,
        uselist: Optional[bool] = ...,
        order_by: _OrderByArgument = ...,
        backref: Union[str, _BackrefResult] = ...,
        back_populates: str = ...,
        overlaps: Union[AbstractSet[str], str] = ...,
        post_update: bool = ...,
        cascade: Union[Literal[False], Sequence[str]] = ...,
        viewonly: bool = ...,
        lazy: str = ...,
        collection_class: Optional[Union[Type[Any], Callable[[], Any]]] = ...,
        passive_deletes: Union[bool, Literal["all"]] = ...,
        passive_updates: bool = ...,
        remote_side: Optional[Any] = ...,
        enable_typechecks: bool = ...,  # NOTE: not documented
        join_depth: Optional[int] = ...,
        comparator_factory: Optional[Any] = ...,
        single_parent: bool = ...,
        innerjoin: Union[bool, str] = ...,
        distinct_target_key: Optional[bool] = ...,
        doc: Optional[str] = ...,
        active_history: bool = ...,
        cascade_backrefs: bool = ...,
        load_on_pending: bool = ...,
        bake_queries: bool = ...,
        _local_remote_pairs: Optional[Any] = ...,
        query_class: Optional[Any] = ...,
        info: Optional[MutableMapping[Any, Any]] = ...,
        omit_join: Optional[Literal[False]] = ...,
        sync_backref: Optional[Any] = ...,
    ) -> None: ...
    def instrument_class(self, mapper: Any) -> None: ...
    class Comparator(PropComparator):
        prop: Any = ...
        def __init__(
            self,
            prop: Any,
            parentmapper: Any,
            adapt_to_entity: Optional[Any] = ...,
            of_type: Optional[Any] = ...,
            extra_criteria: Any = ...,
        ) -> None: ...
        def adapt_to_entity(self, adapt_to_entity: Any): ...
        @util.memoized_property
        def entity(self): ...
        @util.memoized_property
        def mapper(self): ...
        def __clause_element__(self): ...
        def of_type(self, cls: Any): ...
        def and_(self, *other: Any): ...
        def in_(self, other: Any) -> None: ...
        __hash__: Any = ...
        def __eq__(self, other: Any) -> Any: ...
        def any(self, criterion: Optional[Any] = ..., **kwargs: Any): ...
        def has(self, criterion: Optional[Any] = ..., **kwargs: Any): ...
        def contains(self, other: Any, **kwargs: Any): ...
        def __ne__(self, other: Any) -> Any: ...
        @util.memoized_property
        def property(self): ...
    def merge(
        self,
        session: Any,
        source_state: Any,
        source_dict: Any,
        dest_state: Any,
        dest_dict: Any,
        load: Any,
        _recursive: Any,
        _resolve_conflict_map: Any,
    ) -> None: ...
    def cascade_iterator(
        self,
        type_: Any,
        state: Any,
        dict_: Any,
        visited_states: Any,
        halt_on: Optional[Any] = ...,
    ) -> None: ...
    @util.memoized_property
    def entity(self) -> Union[AliasedInsp, Mapper]: ...
    @util.memoized_property
    def mapper(self) -> Mapper: ...
    def do_init(self) -> None: ...
    @property
    def cascade(self) -> CascadeOptions: ...
    @cascade.setter
    def cascade(self, cascade: Sequence[str]) -> None: ...

class JoinCondition:
    parent_persist_selectable: Any = ...
    parent_local_selectable: Any = ...
    child_persist_selectable: Any = ...
    child_local_selectable: Any = ...
    parent_equivalents: Any = ...
    child_equivalents: Any = ...
    primaryjoin: Any = ...
    secondaryjoin: Any = ...
    secondary: Any = ...
    consider_as_foreign_keys: Any = ...
    prop: Any = ...
    self_referential: Any = ...
    support_sync: Any = ...
    can_be_synced_fn: Any = ...
    def __init__(
        self,
        parent_persist_selectable: Any,
        child_persist_selectable: Any,
        parent_local_selectable: Any,
        child_local_selectable: Any,
        primaryjoin: Optional[Any] = ...,
        secondary: Optional[Any] = ...,
        secondaryjoin: Optional[Any] = ...,
        parent_equivalents: Optional[Any] = ...,
        child_equivalents: Optional[Any] = ...,
        consider_as_foreign_keys: Optional[Any] = ...,
        local_remote_pairs: Optional[Any] = ...,
        remote_side: Optional[Any] = ...,
        self_referential: bool = ...,
        prop: Optional[Any] = ...,
        support_sync: bool = ...,
        can_be_synced_fn: Any = ...,
    ): ...
    @property
    def primaryjoin_minus_local(self): ...
    @property
    def secondaryjoin_minus_local(self): ...
    @util.memoized_property
    def primaryjoin_reverse_remote(self): ...
    @util.memoized_property
    def remote_columns(self): ...
    @util.memoized_property
    def local_columns(self): ...
    @util.memoized_property
    def foreign_key_columns(self): ...
    def join_targets(
        self,
        source_selectable: Any,
        dest_selectable: Any,
        aliased: Any,
        single_crit: Optional[Any] = ...,
        extra_criteria: Any = ...,
    ): ...
    def create_lazy_clause(self, reverse_direction: bool = ...): ...

class _ColInAnnotations:
    name: Any = ...
    def __init__(self, name: Any) -> None: ...
    def __call__(self, c: Any): ...

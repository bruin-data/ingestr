# fmt: off
from collections import namedtuple
from typing import Any
from typing import Optional

from . import attributes as attributes
from . import interfaces as interfaces
from . import loading as loading
from . import properties as properties
from . import query as query
from . import relationships as relationships
from . import unitofwork as unitofwork
from .context import ORMCompileState as ORMCompileState
from .context import QueryContext as QueryContext
from .interfaces import LoaderStrategy as LoaderStrategy
from .interfaces import StrategizedProperty as StrategizedProperty
from .state import InstanceState as InstanceState
from .util import aliased as aliased
from .. import event as event
from .. import inspect as inspect
from .. import log as log
from .. import sql as sql
from .. import util as util
from ..sql import visitors as visitors
from ..sql.selectable import LABEL_STYLE_TABLENAME_PLUS_COL as LABEL_STYLE_TABLENAME_PLUS_COL
# fmt: on

class UninstrumentedColumnLoader(LoaderStrategy):
    columns: Any = ...
    def __init__(self, parent: Any, strategy_key: Any) -> None: ...
    def setup_query(
        self,
        compile_state: Any,
        query_entity: Any,
        path: Any,
        loadopt: Any,
        adapter: Any,
        column_collection: Optional[Any] = ...,
        **kwargs: Any,
    ) -> None: ...
    def create_row_processor(
        self,
        context: Any,
        query_entity: Any,
        path: Any,
        loadopt: Any,
        mapper: Any,
        result: Any,
        adapter: Any,
        populators: Any,
    ) -> None: ...

class ColumnLoader(LoaderStrategy):
    columns: Any = ...
    is_composite: Any = ...
    def __init__(self, parent: Any, strategy_key: Any) -> None: ...
    def setup_query(
        self,
        compile_state: Any,
        query_entity: Any,
        path: Any,
        loadopt: Any,
        adapter: Any,
        column_collection: Any,
        memoized_populators: Any,
        check_for_adapt: bool = ...,
        **kwargs: Any,
    ) -> None: ...
    is_class_level: bool = ...
    def init_class_attribute(self, mapper: Any) -> None: ...
    def create_row_processor(
        self,
        context: Any,
        query_entity: Any,
        path: Any,
        loadopt: Any,
        mapper: Any,
        result: Any,
        adapter: Any,
        populators: Any,
    ) -> None: ...

class ExpressionColumnLoader(ColumnLoader):
    def __init__(self, parent: Any, strategy_key: Any) -> None: ...
    def setup_query(
        self,
        compile_state: Any,
        query_entity: Any,
        path: Any,
        loadopt: Any,
        adapter: Any,
        column_collection: Any,
        memoized_populators: Any,
        **kwargs: Any,
    ) -> None: ...
    def create_row_processor(
        self,
        context: Any,
        query_entity: Any,
        path: Any,
        loadopt: Any,
        mapper: Any,
        result: Any,
        adapter: Any,
        populators: Any,
    ) -> None: ...
    is_class_level: bool = ...
    def init_class_attribute(self, mapper: Any) -> None: ...

class DeferredColumnLoader(LoaderStrategy):
    raiseload: Any = ...
    columns: Any = ...
    group: Any = ...
    def __init__(self, parent: Any, strategy_key: Any) -> None: ...
    def create_row_processor(
        self,
        context: Any,
        query_entity: Any,
        path: Any,
        loadopt: Any,
        mapper: Any,
        result: Any,
        adapter: Any,
        populators: Any,
    ) -> None: ...
    is_class_level: bool = ...
    def init_class_attribute(self, mapper: Any) -> None: ...
    def setup_query(
        self,
        compile_state: Any,
        query_entity: Any,
        path: Any,
        loadopt: Any,
        adapter: Any,
        column_collection: Any,
        memoized_populators: Any,
        only_load_props: Optional[Any] = ...,
        **kw: Any,
    ) -> None: ...

class LoadDeferredColumns:
    key: Any = ...
    raiseload: Any = ...
    def __init__(self, key: Any, raiseload: bool = ...) -> None: ...
    def __call__(self, state: Any, passive: Any = ...): ...

class AbstractRelationshipLoader(LoaderStrategy):
    mapper: Any = ...
    entity: Any = ...
    target: Any = ...
    uselist: Any = ...
    def __init__(self, parent: Any, strategy_key: Any) -> None: ...

class DoNothingLoader(LoaderStrategy): ...

class NoLoader(AbstractRelationshipLoader):
    is_class_level: bool = ...
    def init_class_attribute(self, mapper: Any) -> None: ...
    def create_row_processor(
        self,
        context: Any,
        query_entity: Any,
        path: Any,
        loadopt: Any,
        mapper: Any,
        result: Any,
        adapter: Any,
        populators: Any,
    ) -> None: ...

class LazyLoader(AbstractRelationshipLoader, util.MemoizedSlots):
    is_aliased_class: Any = ...
    use_get: Any = ...
    def __init__(self, parent: Any, strategy_key: Any) -> None: ...
    is_class_level: bool = ...
    def init_class_attribute(self, mapper: Any) -> None: ...
    def create_row_processor(
        self,
        context: Any,
        query_entity: Any,
        path: Any,
        loadopt: Any,
        mapper: Any,
        result: Any,
        adapter: Any,
        populators: Any,
    ) -> None: ...

class LoadLazyAttribute:
    key: Any = ...
    strategy_key: Any = ...
    loadopt: Any = ...
    def __init__(
        self, key: Any, initiating_strategy: Any, loadopt: Any
    ) -> None: ...
    def __call__(self, state: Any, passive: Any = ...): ...

class PostLoader(AbstractRelationshipLoader): ...

class ImmediateLoader(PostLoader):
    def init_class_attribute(self, mapper: Any) -> None: ...
    def create_row_processor(
        self,
        context: Any,
        query_entity: Any,
        path: Any,
        loadopt: Any,
        mapper: Any,
        result: Any,
        adapter: Any,
        populators: Any,
    ) -> None: ...

class SubqueryLoader(PostLoader):
    join_depth: Any = ...
    def __init__(self, parent: Any, strategy_key: Any) -> None: ...
    def init_class_attribute(self, mapper: Any) -> None: ...
    class _SubqCollections:
        session: Any = ...
        execution_options: Any = ...
        load_options: Any = ...
        params: Any = ...
        subq: Any = ...
        def __init__(self, context: Any, subq: Any) -> None: ...
        def get(self, key: Any, default: Any): ...
        def loader(self, state: Any, dict_: Any, row: Any) -> None: ...
    def create_row_processor(
        self,
        context: Any,
        query_entity: Any,
        path: Any,
        loadopt: Any,
        mapper: Any,
        result: Any,
        adapter: Any,
        populators: Any,
    ): ...

class JoinedLoader(AbstractRelationshipLoader):
    join_depth: Any = ...
    def __init__(self, parent: Any, strategy_key: Any) -> None: ...
    def init_class_attribute(self, mapper: Any) -> None: ...
    def setup_query(
        self,
        compile_state: Any,
        query_entity: Any,
        path: Any,
        loadopt: Any,
        adapter: Any,
        column_collection: Optional[Any] = ...,
        parentmapper: Optional[Any] = ...,
        chained_from_outerjoin: bool = ...,
        **kwargs: Any,
    ) -> None: ...
    def create_row_processor(
        self,
        context: Any,
        query_entity: Any,
        path: Any,
        loadopt: Any,
        mapper: Any,
        result: Any,
        adapter: Any,
        populators: Any,
    ) -> None: ...

class SelectInLoader(PostLoader, util.MemoizedSlots):

    query_info = namedtuple(
        "queryinfo",
        [
            "load_only_child",
            "load_with_join",
            "in_expr",
            "pk_cols",
            "zero_idx",
            "child_lookup_cols",
        ],
    )
    join_depth: Any = ...
    omit_join: Any = ...
    def __init__(self, parent: Any, strategy_key: Any) -> None: ...
    def init_class_attribute(self, mapper: Any) -> None: ...
    def create_row_processor(
        self,
        context: Any,
        query_entity: Any,
        path: Any,
        loadopt: Any,
        mapper: Any,
        result: Any,
        adapter: Any,
        populators: Any,
    ): ...

def single_parent_validator(desc: Any, prop: Any): ...

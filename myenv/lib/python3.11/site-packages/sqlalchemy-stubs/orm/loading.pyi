# fmt: off
from typing import Any
from typing import Optional

from . import attributes as attributes
from . import path_registry as path_registry
from . import strategy_options as strategy_options
from .util import state_str as state_str
from .. import future as future
from .. import util as util
from ..engine import result_tuple as result_tuple
from ..engine.result import ChunkedIteratorResult as ChunkedIteratorResult
from ..engine.result import FrozenResult as FrozenResult
from ..engine.result import SimpleResultMetaData as SimpleResultMetaData
from ..sql.selectable import LABEL_STYLE_TABLENAME_PLUS_COL as LABEL_STYLE_TABLENAME_PLUS_COL
# fmt: on

def instances(cursor: Any, context: Any): ...
def merge_frozen_result(
    session: Any, statement: Any, frozen_result: Any, load: bool = ...
): ...
def merge_result(query: Any, iterator: Any, load: bool = ...): ...
def get_from_identity(session: Any, mapper: Any, key: Any, passive: Any): ...
def load_on_ident(
    session: Any,
    statement: Any,
    key: Any,
    load_options: Optional[Any] = ...,
    refresh_state: Optional[Any] = ...,
    with_for_update: Optional[Any] = ...,
    only_load_props: Optional[Any] = ...,
    no_autoflush: bool = ...,
    bind_arguments: Any = ...,
    execution_options: Any = ...,
): ...
def load_on_pk_identity(
    session: Any,
    statement: Any,
    primary_key_identity: Any,
    load_options: Optional[Any] = ...,
    refresh_state: Optional[Any] = ...,
    with_for_update: Optional[Any] = ...,
    only_load_props: Optional[Any] = ...,
    identity_token: Optional[Any] = ...,
    no_autoflush: bool = ...,
    bind_arguments: Any = ...,
    execution_options: Any = ...,
): ...

class PostLoad:
    loaders: Any = ...
    states: Any = ...
    load_keys: Any = ...
    def __init__(self) -> None: ...
    def add_state(self, state: Any, overwrite: Any) -> None: ...
    def invoke(self, context: Any, path: Any) -> None: ...
    @classmethod
    def for_context(cls, context: Any, path: Any, only_load_props: Any): ...
    @classmethod
    def path_exists(self, context: Any, path: Any, key: Any): ...
    @classmethod
    def callable_for_path(
        cls,
        context: Any,
        path: Any,
        limit_to_mapper: Any,
        token: Any,
        loader_callable: Any,
        *arg: Any,
        **kw: Any,
    ) -> None: ...

def load_scalar_attributes(
    mapper: Any, state: Any, attribute_names: Any, passive: Any
) -> None: ...

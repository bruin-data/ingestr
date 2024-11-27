# fmt: off
from typing import Any

from . import attributes as attributes
from . import evaluator as evaluator
from . import loading as loading
from . import sync as sync
from .base import state_str as state_str
from .. import future as future
from .. import sql as sql
from .. import util as util
from ..sql import coercions as coercions
from ..sql import expression as expression
from ..sql import operators as operators
from ..sql import roles as roles
from ..sql import select as select
from ..sql.base import CompileState as CompileState
from ..sql.base import Options as Options
from ..sql.dml import DeleteDMLState as DeleteDMLState
from ..sql.dml import UpdateDMLState as UpdateDMLState
from ..sql.elements import BooleanClauseList as BooleanClauseList
from ..sql.selectable import LABEL_STYLE_TABLENAME_PLUS_COL as LABEL_STYLE_TABLENAME_PLUS_COL
# fmt: on

def save_obj(
    base_mapper: Any, states: Any, uowtransaction: Any, single: bool = ...
) -> None: ...
def post_update(
    base_mapper: Any, states: Any, uowtransaction: Any, post_update_cols: Any
) -> None: ...
def delete_obj(base_mapper: Any, states: Any, uowtransaction: Any) -> None: ...

class BulkUDCompileState(CompileState):
    class default_update_options(Options): ...
    @classmethod
    def orm_pre_session_exec(
        cls,
        session: Any,
        statement: Any,
        params: Any,
        execution_options: Any,
        bind_arguments: Any,
        is_reentrant_invoke: Any,
    ): ...
    @classmethod
    def orm_setup_cursor_result(
        cls,
        session: Any,
        statement: Any,
        params: Any,
        execution_options: Any,
        bind_arguments: Any,
        result: Any,
    ): ...

class BulkORMUpdate(UpdateDMLState, BulkUDCompileState):
    mapper: Any = ...
    extra_criteria_entities: Any = ...
    @classmethod
    def create_for_statement(
        cls, statement: Any, compiler: Any, **kw: Any
    ): ...

class BulkORMDelete(DeleteDMLState, BulkUDCompileState):
    mapper: Any = ...
    extra_criteria_entities: Any = ...
    @classmethod
    def create_for_statement(
        cls, statement: Any, compiler: Any, **kw: Any
    ): ...

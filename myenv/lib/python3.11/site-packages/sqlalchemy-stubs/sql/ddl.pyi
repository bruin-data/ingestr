from typing import Any
from typing import Callable
from typing import Dict
from typing import Iterable
from typing import List
from typing import Optional
from typing import Sequence
from typing import Set
from typing import Tuple
from typing import TypeVar
from typing import Union

from typing_extensions import Protocol

from . import roles
from .base import Executable
from .base import SchemaVisitor
from .elements import ClauseElement
from .schema import ForeignKey
from .schema import ForeignKeyConstraint
from .schema import Index
from .schema import SchemaItem
from .schema import Table
from ..engine import Connection
from ..engine import Dialect
from ..engine import Engine

_DDLE = TypeVar("_DDLE", bound=DDLElement)

_Bind = Union[Engine, Connection]

class _DDLElementCallback(Protocol):
    def __call__(
        self,
        __ddl: DDLElement,
        __target: Optional[SchemaItem],
        __bind: _Bind,
        *,
        state: Optional[Any],
        **kw: Any,
    ) -> bool: ...

class _DDLCompiles(ClauseElement): ...

class DDLElement(roles.DDLRole, Executable, _DDLCompiles):
    target: Optional[SchemaItem] = ...
    on: Any = ...
    dialect: Union[str, Tuple[str, ...], List[str], Set[str], None] = ...
    callable_: Optional[_DDLElementCallback] = ...
    state: Optional[Any] = ...
    bind: Optional[_Bind] = ...
    def execute(  # type: ignore[override]
        self,
        bind: Optional[_Bind] = ...,
        target: Optional[SchemaItem] = ...,
    ) -> Any: ...
    def against(self: _DDLE, target: Optional[SchemaItem]) -> _DDLE: ...
    def execute_if(
        self: _DDLE,
        dialect: Optional[
            Union[str, Tuple[str, ...], List[str], Set[str]]
        ] = ...,
        callable_: Optional[_DDLElementCallback] = ...,
        state: Optional[Any] = ...,
    ) -> _DDLE: ...
    def __call__(
        self, target: SchemaItem, bind: _Bind, **kw: Any
    ) -> Optional[Any]: ...

class DDL(DDLElement):
    __visit_name__: str = ...
    statement: str = ...
    context: Any = ...
    def __init__(
        self,
        statement: str,
        context: Optional[Dict[str, Any]] = ...,
        bind: Optional[_Bind] = ...,
    ) -> None: ...

class _CreateDropBase(DDLElement):
    element: Any = ...
    if_exists: bool = ...
    if_not_exists: bool = ...
    def __init__(
        self,
        element: Any,
        bind: Optional[_Bind] = ...,
        if_exists: bool = ...,
        if_not_exists: bool = ...,
        _legacy_bind: Optional[Any] = ...,
    ) -> None: ...
    @property
    def stringify_dialect(self) -> str: ...  # type: ignore[override]

class CreateSchema(_CreateDropBase):
    __visit_name__: str = ...
    quote: Any = ...
    def __init__(
        self, name: str, quote: Optional[Any] = ..., **kw: Any
    ) -> None: ...

class DropSchema(_CreateDropBase):
    __visit_name__: str = ...
    quote: Any = ...
    cascade: Any = ...
    def __init__(
        self,
        name: str,
        quote: Optional[Any] = ...,
        cascade: bool = ...,
        **kw: Any,
    ) -> None: ...

class CreateTable(_CreateDropBase):
    __visit_name__: str = ...
    columns: Any = ...
    include_foreign_key_constraints: Any = ...
    def __init__(
        self,
        element: Table,
        bind: Optional[_Bind] = ...,
        include_foreign_key_constraints: Optional[
            Sequence[ForeignKeyConstraint]
        ] = ...,
        if_not_exists: bool = ...,
    ) -> None: ...

class _DropView(_CreateDropBase):
    __visit_name__: str = ...

class CreateColumn(_DDLCompiles):
    __visit_name__: str = ...
    element: Any = ...
    def __init__(self, element: Any) -> None: ...

class DropTable(_CreateDropBase):
    __visit_name__: str = ...
    def __init__(
        self,
        element: Table,
        bind: Optional[_Bind] = ...,
        if_exists: bool = ...,
    ) -> None: ...

class CreateSequence(_CreateDropBase):
    __visit_name__: str = ...

class DropSequence(_CreateDropBase):
    __visit_name__: str = ...

class CreateIndex(_CreateDropBase):
    __visit_name__: str = ...
    def __init__(
        self,
        element: Index,
        bind: Optional[_Bind] = ...,
        if_not_exists: bool = ...,
    ) -> None: ...

class DropIndex(_CreateDropBase):
    __visit_name__: str = ...
    def __init__(
        self,
        element: Index,
        bind: Optional[_Bind] = ...,
        if_exists: bool = ...,
    ) -> None: ...

class AddConstraint(_CreateDropBase):
    __visit_name__: str = ...
    def __init__(self, element: Any, *args: Any, **kw: Any) -> None: ...

class DropConstraint(_CreateDropBase):
    __visit_name__: str = ...
    cascade: Any = ...
    def __init__(
        self, element: Any, cascade: bool = ..., **kw: Any
    ) -> None: ...

class SetTableComment(_CreateDropBase):
    __visit_name__: str = ...

class DropTableComment(_CreateDropBase):
    __visit_name__: str = ...

class SetColumnComment(_CreateDropBase):
    __visit_name__: str = ...

class DropColumnComment(_CreateDropBase):
    __visit_name__: str = ...

class DDLBase(SchemaVisitor):
    connection: Any = ...
    def __init__(self, connection: Any) -> None: ...

class SchemaGenerator(DDLBase):
    checkfirst: Any = ...
    tables: Any = ...
    preparer: Any = ...
    dialect: Dialect = ...
    memo: Any = ...
    def __init__(
        self,
        dialect: Dialect,
        connection: Any,
        checkfirst: bool = ...,
        tables: Optional[Any] = ...,
        **kwargs: Any,
    ) -> None: ...
    def visit_metadata(self, metadata: Any) -> None: ...
    def visit_table(
        self,
        table: Any,
        create_ok: bool = ...,
        include_foreign_key_constraints: Optional[Any] = ...,
        _is_metadata_operation: bool = ...,
    ) -> None: ...
    def visit_foreign_key_constraint(self, constraint: Any) -> None: ...
    def visit_sequence(self, sequence: Any, create_ok: bool = ...) -> None: ...
    def visit_index(self, index: Any, create_ok: bool = ...) -> None: ...

class SchemaDropper(DDLBase):
    checkfirst: Any = ...
    tables: Any = ...
    preparer: Any = ...
    dialect: Dialect = ...
    memo: Any = ...
    def __init__(
        self,
        dialect: Dialect,
        connection: Any,
        checkfirst: bool = ...,
        tables: Optional[Any] = ...,
        **kwargs: Any,
    ) -> None: ...
    def visit_metadata(self, metadata: Any) -> None: ...
    def visit_index(self, index: Any, drop_ok: bool = ...) -> None: ...
    def visit_table(
        self,
        table: Any,
        drop_ok: bool = ...,
        _is_metadata_operation: bool = ...,
    ) -> None: ...
    def visit_foreign_key_constraint(self, constraint: Any) -> None: ...
    def visit_sequence(self, sequence: Any, drop_ok: bool = ...) -> None: ...

def sort_tables(
    tables: Iterable[Table],
    skip_fn: Optional[Callable[[ForeignKey], bool]] = ...,
    extra_dependencies: Optional[Iterable[Tuple[Table, Table]]] = ...,
) -> List[Table]: ...
def sort_tables_and_constraints(
    tables: Iterable[Table],
    filter_fn: Optional[
        Callable[[ForeignKeyConstraint], Optional[bool]]
    ] = ...,
    extra_dependencies: Optional[Iterable[Tuple[Table, Table]]] = ...,
    _warn_for_cycles: bool = ...,
) -> List[Tuple[Optional[Table], List[ForeignKeyConstraint]]]: ...

TypingCreateDropBase = _CreateDropBase

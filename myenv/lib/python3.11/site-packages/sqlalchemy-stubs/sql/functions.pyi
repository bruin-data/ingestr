from typing import Any
from typing import Generic
from typing import Optional
from typing import overload
from typing import Sequence
from typing import Type
from typing import TypeVar
from typing import Union

from typing_extensions import Protocol

from . import roles
from . import sqltypes
from . import type_api
from .base import ColumnCollection
from .base import Executable
from .base import Generative
from .base import HasMemoized
from .elements import AsBoolean
from .elements import BinaryExpression
from .elements import ClauseElement
from .elements import ClauseList
from .elements import ColumnElement
from .elements import FunctionFilter
from .elements import Grouping
from .elements import NamedColumn
from .elements import Over
from .elements import TableValuedColumn
from .elements import WithinGroup
from .schema import Sequence
from .selectable import FromClause
from .selectable import Select
from .selectable import TableValuedAlias
from .visitors import TraversibleType

_T_co = TypeVar("_T_co", covariant=True)
_TE = TypeVar("_TE", bound=type_api.TypeEngine[Any])
_FE = TypeVar("_FE", bound=FunctionElement[Any])

_OverByType = Union[ClauseElement, str, roles.OrderByRole]

def register_function(
    identifier: str, fn: Any, package: str = ...
) -> None: ...

class FunctionElement(  # type: ignore[misc]
    Executable, ColumnElement[_TE], FromClause, Generative, Generic[_TE]
):
    packagenames: Any = ...
    clause_expr: Any = ...
    def __init__(self, *clauses: Any, **kwargs: Any) -> None: ...
    @overload
    def scalar_table_valued(
        self, name: Any, type_: None = ...
    ) -> ScalarFunctionColumn[sqltypes.NullType]: ...
    @overload
    def scalar_table_valued(
        self, name: Any, type_: Union[_TE, Type[_TE]]
    ) -> ScalarFunctionColumn[_TE]: ...
    def table_valued(self, *expr: Any, **kw: Any) -> TableValuedAlias: ...  # type: ignore[override]
    def column_valued(
        self, name: Optional[Any] = ..., joins_implicitly: bool = False
    ) -> TableValuedColumn[Any]: ...
    @property
    def columns(self) -> ColumnCollection[ColumnElement[Any]]: ...  # type: ignore[override]
    @HasMemoized.memoized_attribute
    def clauses(self) -> ClauseList[Any]: ...
    def over(
        self,
        partition_by: Optional[
            Union[_OverByType, Sequence[_OverByType]]
        ] = ...,
        order_by: Optional[Union[_OverByType, Sequence[_OverByType]]] = ...,
        rows: Optional[Any] = ...,
        range_: Optional[Any] = ...,
    ) -> Over[_TE]: ...
    def within_group(self, *order_by: Any) -> WithinGroup[_TE]: ...
    @overload
    def filter(self: _FE) -> _FE: ...
    @overload
    def filter(
        self, __criteria: Any, *criterion: Any
    ) -> FunctionFilter[_TE]: ...
    def as_comparison(
        self, left_index: int, right_index: int
    ) -> FunctionAsBinary: ...
    def within_group_type(
        self, within_group: Any
    ) -> Optional[type_api.TypeEngine[Any]]: ...
    def alias(self, name: Optional[Any] = ...) -> TableValuedAlias: ...  # type: ignore[override]
    def select(self) -> Select: ...  # type: ignore[override]
    def scalar(self) -> Any: ...  # type: ignore[override]
    def execute(self) -> Any: ...  # type: ignore[override]
    def self_group(
        self: _FE, against: Optional[Any] = ...
    ) -> Union[_FE, Grouping[_TE], AsBoolean[_FE]]: ...

class FunctionAsBinary(BinaryExpression[sqltypes.Boolean]):
    sql_function: FunctionElement[Any] = ...
    left_index: int = ...
    right_index: int = ...
    operator: Any = ...
    type: sqltypes.Boolean = ...
    negate: Any = ...
    modifiers: Any = ...
    left: ClauseElement = ...
    right: ClauseElement = ...
    def __init__(
        self, fn: FunctionElement[Any], left_index: int, right_index: int
    ) -> None: ...

class ScalarFunctionColumn(NamedColumn[_TE]):
    __visit_name__: str = ...
    is_literal: bool = ...
    table: Any = ...
    fn: ClauseElement = ...
    name: Any = ...
    type: _TE = ...  # type: ignore[assignment]
    @overload
    def __init__(
        self: ScalarFunctionColumn[sqltypes.NullType],
        fn: ClauseElement,
        name: str,
        type_: None = ...,
    ) -> None: ...
    @overload
    def __init__(
        self, fn: ClauseElement, name: str, type_: Union[_TE, Type[_TE]]
    ) -> None: ...

class _FunctionGenerator:
    opts: Any = ...
    def __init__(self, **opts: Any) -> None: ...
    def __getattr__(self, name: str) -> _FunctionGenerator: ...
    @overload
    def __call__(
        self, *c: Any, type_: None = ..., **kwargs: Any
    ) -> Function[sqltypes.NullType]: ...
    @overload
    def __call__(
        self, *c: Any, type_: Union[_TE, Type[_TE]], **kwargs: Any
    ) -> Function[_TE]: ...

func: _FunctionGenerator
modifier: _FunctionGenerator

class _TypeDescriptor(Protocol[_T_co]):
    @overload
    def __get__(self, instance: None, owner: Any) -> _T_co: ...
    @overload
    def __get__(self, instance: GenericFunction[_TE], owner: Any) -> _TE: ...

class Function(FunctionElement[_TE]):
    __visit_name__: str = ...
    type: _TypeDescriptor[sqltypes.NullType] = ...  # type: ignore[assignment]
    packagenames: Any = ...
    name: Any = ...
    @overload
    def __init__(
        self: Function[sqltypes.NullType],
        name: Any,
        *clauses: Any,
        type_: None = ...,
        **kw: Any,
    ) -> None: ...
    @overload
    def __init__(
        self, name: Any, *clauses: Any, type_: Union[_TE, Type[_TE]], **kw: Any
    ) -> None: ...

class _GenericMeta(TraversibleType):
    def __init__(cls, clsname: Any, bases: Any, clsdict: Any) -> None: ...

class GenericFunction(Function[_TE], metaclass=_GenericMeta):
    coerce_arguments: bool = ...
    inherit_cache: bool = ...
    packagenames: Any = ...
    clause_expr: Any = ...
    type: _TypeDescriptor[sqltypes.NullType] = ...
    @overload
    def __init__(
        self: GenericFunction[sqltypes.NullType],
        *args: Any,
        type_: None = ...,
        **kwargs: Any,
    ) -> None: ...
    @overload
    def __init__(
        self, *args: Any, type_: Union[_TE, Type[_TE]], **kwargs: Any
    ) -> None: ...

class next_value(GenericFunction[_TE]):
    type: _TypeDescriptor[sqltypes.Integer] = ...  # type: ignore[assignment]
    name: str = ...
    sequence: Sequence[_TE] = ...
    def __init__(self, seq: Sequence[_TE], **kw: Any) -> None: ...
    def compare(self, other: Any, **kw: Any) -> bool: ...

class AnsiFunction(GenericFunction[_TE]):
    inherit_cache: bool = ...

class ReturnTypeFromArgs(GenericFunction[_TE]):
    inherit_cache: bool = ...
    def __init__(self, *args: Any, **kwargs: Any) -> None: ...

class coalesce(ReturnTypeFromArgs[_TE]):
    inherit_cache: bool = ...

class max(ReturnTypeFromArgs[_TE]):
    inherit_cache: bool = ...

class min(ReturnTypeFromArgs[_TE]):
    inherit_cache: bool = ...

class sum(ReturnTypeFromArgs[_TE]):
    inherit_cache: bool = ...

class now(GenericFunction[_TE]):
    type: _TypeDescriptor[Type[sqltypes.DateTime]] = ...  # type: ignore[assignment]
    inherit_cache: bool = ...
    @overload
    def __init__(
        self: now[sqltypes.DateTime],
        *args: Any,
        type_: None = ...,
        **kwargs: Any,
    ) -> None: ...
    @overload
    def __init__(
        self, *args: Any, type_: Union[_TE, Type[_TE]], **kwargs: Any
    ) -> None: ...

class concat(GenericFunction[_TE]):
    type: _TypeDescriptor[Type[sqltypes.String]] = ...  # type: ignore[assignment]
    inherit_cache: bool = ...
    @overload
    def __init__(
        self: concat[sqltypes.String],
        *args: Any,
        type_: None = ...,
        **kwargs: Any,
    ) -> None: ...
    @overload
    def __init__(
        self, *args: Any, type_: Union[_TE, Type[_TE]], **kwargs: Any
    ) -> None: ...

class char_length(GenericFunction[_TE]):
    type: _TypeDescriptor[Type[sqltypes.Integer]] = ...  # type: ignore[assignment]
    inherit_cache: bool = ...
    @overload
    def __init__(
        self: char_length[sqltypes.Integer],
        arg: Any,
        type_: None = ...,
        **kwargs: Any,
    ) -> None: ...
    @overload
    def __init__(
        self, arg: Any, type_: Union[_TE, Type[_TE]], **kwargs: Any
    ) -> None: ...

class random(GenericFunction[_TE]):
    inherit_cache: bool = ...

class count(GenericFunction[_TE]):
    type: _TypeDescriptor[Type[sqltypes.Integer]] = ...  # type: ignore[assignment]
    inherit_cache: bool = ...
    @overload
    def __init__(
        self: count[sqltypes.Integer],
        expression: Optional[Any] = ...,
        *,
        type_: None = ...,
        **kwargs: Any,
    ) -> None: ...
    @overload
    def __init__(
        self,
        expression: Optional[Any] = ...,
        *,
        type_: Union[_TE, Type[_TE]],
        **kwargs: Any,
    ) -> None: ...

class current_date(AnsiFunction[_TE]):
    type: _TypeDescriptor[Type[sqltypes.Date]] = ...  # type: ignore[assignment]
    inherit_cache: bool = ...
    @overload
    def __init__(
        self: current_date[sqltypes.Date],
        *args: Any,
        type_: None = ...,
        **kwargs: Any,
    ) -> None: ...
    @overload
    def __init__(
        self, *args: Any, type_: Union[_TE, Type[_TE]], **kwargs: Any
    ) -> None: ...

class current_time(AnsiFunction[_TE]):
    type: _TypeDescriptor[Type[sqltypes.Time]] = ...  # type: ignore[assignment]
    inherit_cache: bool = ...
    @overload
    def __init__(
        self: current_time[sqltypes.Time],
        *args: Any,
        type_: None = ...,
        **kwargs: Any,
    ) -> None: ...
    @overload
    def __init__(
        self, *args: Any, type_: Union[_TE, Type[_TE]], **kwargs: Any
    ) -> None: ...

class current_timestamp(AnsiFunction[_TE]):
    type: _TypeDescriptor[Type[sqltypes.DateTime]] = ...  # type: ignore[assignment]
    inherit_cache: bool = ...
    @overload
    def __init__(
        self: current_timestamp[sqltypes.DateTime],
        *args: Any,
        type_: None = ...,
        **kwargs: Any,
    ) -> None: ...
    @overload
    def __init__(
        self, *args: Any, type_: Union[_TE, Type[_TE]], **kwargs: Any
    ) -> None: ...

class current_user(AnsiFunction[_TE]):
    type: _TypeDescriptor[Type[sqltypes.String]] = ...  # type: ignore[assignment]
    inherit_cache: bool = ...
    @overload
    def __init__(
        self: current_user[sqltypes.String],
        *args: Any,
        type_: None = ...,
        **kwargs: Any,
    ) -> None: ...
    @overload
    def __init__(
        self, *args: Any, type_: Union[_TE, Type[_TE]], **kwargs: Any
    ) -> None: ...

class localtime(AnsiFunction[_TE]):
    type: _TypeDescriptor[Type[sqltypes.DateTime]] = ...  # type: ignore[assignment]
    inherit_cache: bool = ...
    @overload
    def __init__(
        self: localtime[sqltypes.DateTime],
        *args: Any,
        type_: None = ...,
        **kwargs: Any,
    ) -> None: ...
    @overload
    def __init__(
        self, *args: Any, type_: Union[_TE, Type[_TE]], **kwargs: Any
    ) -> None: ...

class localtimestamp(AnsiFunction[_TE]):
    type: _TypeDescriptor[Type[sqltypes.DateTime]] = ...  # type: ignore[assignment]
    inherit_cache: bool = ...
    @overload
    def __init__(
        self: localtimestamp[sqltypes.DateTime],
        *args: Any,
        type_: None = ...,
        **kwargs: Any,
    ) -> None: ...
    @overload
    def __init__(
        self, *args: Any, type_: Union[_TE, Type[_TE]], **kwargs: Any
    ) -> None: ...

class session_user(AnsiFunction[_TE]):
    type: _TypeDescriptor[Type[sqltypes.String]] = ...  # type: ignore[assignment]
    inherit_cache: bool = ...
    @overload
    def __init__(
        self: session_user[sqltypes.String],
        *args: Any,
        type_: None = ...,
        **kwargs: Any,
    ) -> None: ...
    @overload
    def __init__(
        self, *args: Any, type_: Union[_TE, Type[_TE]], **kwargs: Any
    ) -> None: ...

class sysdate(AnsiFunction[_TE]):
    type: _TypeDescriptor[Type[sqltypes.DateTime]] = ...  # type: ignore[assignment]
    inherit_cache: bool = ...
    @overload
    def __init__(
        self: sysdate[sqltypes.DateTime],
        *args: Any,
        type_: None = ...,
        **kwargs: Any,
    ) -> None: ...
    @overload
    def __init__(
        self, *args: Any, type_: Union[_TE, Type[_TE]], **kwargs: Any
    ) -> None: ...

class user(AnsiFunction[_TE]):
    type: _TypeDescriptor[Type[sqltypes.String]] = ...  # type: ignore[assignment]
    inherit_cache: bool = ...
    @overload
    def __init__(
        self: user[sqltypes.String],
        *args: Any,
        type_: None = ...,
        **kwargs: Any,
    ) -> None: ...
    @overload
    def __init__(
        self, *args: Any, type_: Union[_TE, Type[_TE]], **kwargs: Any
    ) -> None: ...

class array_agg(GenericFunction[_TE]):
    type: _TypeDescriptor[Type[sqltypes.ARRAY[Any]]] = ...  # type: ignore[assignment]
    inherit_cache: bool = ...
    @overload
    def __init__(
        self: array_agg[sqltypes.ARRAY[Any]],
        *args: Any,
        type_: None = ...,
        **kwargs: Any,
    ) -> None: ...
    @overload
    def __init__(
        self, *args: Any, type_: Union[_TE, Type[_TE]], **kwargs: Any
    ) -> None: ...

class OrderedSetAgg(GenericFunction[_TE]):
    array_for_multi_clause: bool = ...
    inherit_cache: bool = ...
    def within_group_type(
        self, within_group: Any
    ) -> type_api.TypeEngine[Any]: ...

class mode(OrderedSetAgg[_TE]):
    inherit_cache: bool = ...

class percentile_cont(OrderedSetAgg[_TE]):
    array_for_multi_clause: bool = ...
    inherit_cache: bool = ...

class percentile_disc(OrderedSetAgg[_TE]):
    array_for_multi_clause: bool = ...
    inherit_cache: bool = ...

class rank(GenericFunction[_TE]):
    type: _TypeDescriptor[sqltypes.Integer] = ...  # type: ignore[assignment]
    inherit_cache: bool = ...
    @overload
    def __init__(
        self: rank[sqltypes.Integer],
        *args: Any,
        type_: None = ...,
        **kwargs: Any,
    ) -> None: ...
    @overload
    def __init__(
        self, *args: Any, type_: Union[_TE, Type[_TE]], **kwargs: Any
    ) -> None: ...

class dense_rank(GenericFunction[_TE]):
    type: _TypeDescriptor[sqltypes.Integer] = ...  # type: ignore[assignment]
    inherit_cache: bool = ...
    @overload
    def __init__(
        self: dense_rank[sqltypes.Integer],
        *args: Any,
        type_: None = ...,
        **kwargs: Any,
    ) -> None: ...
    @overload
    def __init__(
        self, *args: Any, type_: Union[_TE, Type[_TE]], **kwargs: Any
    ) -> None: ...

class percent_rank(GenericFunction[_TE]):
    type: _TypeDescriptor[sqltypes.Numeric] = ...  # type: ignore[assignment]
    inherit_cache: bool = ...
    @overload
    def __init__(
        self: percent_rank[sqltypes.Numeric],
        *args: Any,
        type_: None = ...,
        **kwargs: Any,
    ) -> None: ...
    @overload
    def __init__(
        self, *args: Any, type_: Union[_TE, Type[_TE]], **kwargs: Any
    ) -> None: ...

class cume_dist(GenericFunction[_TE]):
    type: _TypeDescriptor[sqltypes.Numeric] = ...  # type: ignore[assignment]
    inherit_cache: bool = ...
    @overload
    def __init__(
        self: cume_dist[sqltypes.Numeric],
        *args: Any,
        type_: None = ...,
        **kwargs: Any,
    ) -> None: ...
    @overload
    def __init__(
        self, *args: Any, type_: Union[_TE, Type[_TE]], **kwargs: Any
    ) -> None: ...

class cube(GenericFunction[_TE]):
    inherit_cache: bool = ...

class rollup(GenericFunction[_TE]):
    inherit_cache: bool = ...

class grouping_sets(GenericFunction[_TE]):
    inherit_cache: bool = ...

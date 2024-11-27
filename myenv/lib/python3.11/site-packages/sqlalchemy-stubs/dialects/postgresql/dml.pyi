from typing import Any
from typing import Mapping
from typing import Optional
from typing import Sequence
from typing import Union

from . import ExcludeConstraint
from ... import Column
from ... import Constraint
from ... import Index
from ... import util as util
from ...sql.dml import Insert as StandardInsert
from ...sql.elements import ClauseElement
from ...sql.elements import ColumnElement
from ...sql.functions import GenericFunction

class Insert(StandardInsert):
    stringify_dialect: str = ...
    @util.memoized_property
    def excluded(self): ...
    def on_conflict_do_update(
        self,
        constraint: Optional[
            Union[str, Index, Constraint, ExcludeConstraint]
        ] = ...,
        index_elements: Sequence[Union[str, Column]] = ...,
        index_where: Optional[ClauseElement] = ...,
        set_: Mapping[str, Union[ColumnElement, GenericFunction]] = ...,
        where: Optional[ClauseElement] = ...,
    ) -> "Insert": ...
    def on_conflict_do_nothing(
        self,
        constraint: Optional[
            Union[str, Index, Constraint, ExcludeConstraint]
        ] = ...,
        index_elements: Optional[Sequence[Union[str, Column]]] = ...,
        index_where: Optional[Any] = ...,
    ) -> "Insert": ...

insert: Any

class OnConflictClause(ClauseElement):
    stringify_dialect: str = ...
    constraint_target: Any = ...
    inferred_target_elements: Any = ...
    inferred_target_whereclause: Any = ...
    def __init__(
        self,
        constraint: Optional[
            Union[str, Index, Constraint, ExcludeConstraint]
        ] = ...,
        index_elements: Optional[Sequence[Union[str, Column]]] = ...,
        index_where: Optional[Any] = ...,
    ) -> None: ...

class OnConflictDoNothing(OnConflictClause):
    __visit_name__: str = ...

class OnConflictDoUpdate(OnConflictClause):
    __visit_name__: str = ...
    update_values_to_set: Any = ...
    update_whereclause: Any = ...
    def __init__(
        self,
        constraint: Optional[
            Union[str, Index, Constraint, ExcludeConstraint]
        ] = ...,
        index_elements: Optional[Sequence[Union[str, Column]]] = ...,
        index_where: Optional[Any] = ...,
        set_: Mapping[str, Union[ColumnElement, GenericFunction]] = ...,
        where: Optional[ClauseElement] = ...,
    ) -> None: ...

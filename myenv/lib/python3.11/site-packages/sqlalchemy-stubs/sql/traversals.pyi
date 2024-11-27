from typing import Any
from typing import Deque
from typing import Dict
from typing import Iterable
from typing import Iterator
from typing import List
from typing import Mapping
from typing import NamedTuple
from typing import NoReturn
from typing import Optional
from typing import Set
from typing import Tuple
from typing import TypeVar
from typing import Union

from .elements import ClauseElement
from .visitors import ExtendedInternalTraversal
from .visitors import InternalTraversal
from .. import util
from ..util import HasMemoized
from ..util import langhelpers

_T = TypeVar("_T")
_KT = TypeVar("_KT")
_VT = TypeVar("_VT")
_CE = TypeVar("_CE", bound=ClauseElement)

SKIP_TRAVERSE: langhelpers.TypingSymbol
COMPARE_FAILED: bool
COMPARE_SUCCEEDED: bool
NO_CACHE: langhelpers.TypingSymbol
CACHE_IN_PLACE: langhelpers.TypingSymbol
CALL_GEN_CACHE_KEY: langhelpers.TypingSymbol
STATIC_CACHE_KEY: langhelpers.TypingSymbol
PROPAGATE_ATTRS: langhelpers.TypingSymbol
ANON_NAME: langhelpers.TypingSymbol

def compare(obj1: Any, obj2: Any, **kw: Any) -> bool: ...

class HasCacheKey: ...
class MemoizedHasCacheKey(HasCacheKey, HasMemoized): ...

class CacheKey(NamedTuple):
    key: Any
    bindparams: Any
    def to_offline_string(
        self, statement_cache: Any, statement: Any, parameters: Any
    ) -> str: ...
    def __eq__(self, other: object) -> bool: ...

class _CacheKey(ExtendedInternalTraversal):
    visit_has_cache_key: langhelpers.TypingSymbol = ...
    visit_clauseelement: langhelpers.TypingSymbol = ...
    visit_clauseelement_list: langhelpers.TypingSymbol = ...
    visit_annotations_key: langhelpers.TypingSymbol = ...
    visit_clauseelement_tuple: langhelpers.TypingSymbol = ...
    visit_string: langhelpers.TypingSymbol = ...
    visit_boolean: langhelpers.TypingSymbol = ...
    visit_operator: langhelpers.TypingSymbol = ...
    visit_plain_obj: langhelpers.TypingSymbol = ...
    visit_statement_hint_list: langhelpers.TypingSymbol = ...
    visit_type: langhelpers.TypingSymbol = ...
    visit_anon_name: langhelpers.TypingSymbol = ...
    visit_propagate_attrs: langhelpers.TypingSymbol = ...
    def visit_inspectable(
        self,
        attrname: str,
        obj: Any,
        parent: Any,
        anon_map: Any,
        bindparams: Any,
    ) -> Tuple[str, Any]: ...
    def visit_string_list(
        self,
        attrname: Any,
        obj: Iterable[_T],
        parent: Any,
        anon_map: Any,
        bindparams: Any,
    ) -> Tuple[_T, ...]: ...
    def visit_multi(
        self,
        attrname: str,
        obj: Any,
        parent: Any,
        anon_map: Any,
        bindparams: Any,
    ) -> Tuple[str, Any]: ...
    def visit_multi_list(
        self,
        attrname: str,
        obj: Any,
        parent: Any,
        anon_map: Any,
        bindparams: Any,
    ) -> Tuple[str, Tuple[Any, ...]]: ...
    def visit_has_cache_key_tuples(
        self,
        attrname: str,
        obj: Any,
        parent: Any,
        anon_map: Any,
        bindparams: Any,
    ) -> Tuple[Any, ...]: ...
    def visit_has_cache_key_list(
        self,
        attrname: str,
        obj: Any,
        parent: Any,
        anon_map: Any,
        bindparams: Any,
    ) -> Tuple[Any, ...]: ...
    def visit_executable_options(
        self,
        attrname: str,
        obj: Any,
        parent: Any,
        anon_map: Any,
        bindparams: Any,
    ) -> Tuple[Any, ...]: ...
    def visit_inspectable_list(
        self,
        attrname: str,
        obj: Any,
        parent: Any,
        anon_map: Any,
        bindparams: Any,
    ) -> Tuple[Any, ...]: ...
    def visit_clauseelement_tuples(
        self,
        attrname: str,
        obj: Any,
        parent: Any,
        anon_map: Any,
        bindparams: Any,
    ) -> Tuple[Any, ...]: ...
    def visit_fromclause_ordered_set(
        self,
        attrname: str,
        obj: Any,
        parent: Any,
        anon_map: Any,
        bindparams: Any,
    ) -> Tuple[Any, ...]: ...
    def visit_clauseelement_unordered_set(
        self,
        attrname: Any,
        obj: Any,
        parent: Any,
        anon_map: Any,
        bindparams: Any,
    ) -> Tuple[Any, ...]: ...
    def visit_named_ddl_element(
        self,
        attrname: Any,
        obj: Any,
        parent: Any,
        anon_map: Any,
        bindparams: Any,
    ) -> Tuple[Any, ...]: ...
    def visit_prefix_sequence(
        self,
        attrname: Any,
        obj: Any,
        parent: Any,
        anon_map: Any,
        bindparams: Any,
    ) -> Tuple[Any, ...]: ...
    def visit_setup_join_tuple(
        self,
        attrname: Any,
        obj: Any,
        parent: Any,
        anon_map: Any,
        bindparams: Any,
    ) -> Tuple[Any, ...]: ...
    def visit_table_hint_list(
        self,
        attrname: Any,
        obj: Any,
        parent: Any,
        anon_map: Any,
        bindparams: Any,
    ) -> Tuple[Any, ...]: ...
    def visit_plain_dict(
        self,
        attrname: Any,
        obj: Any,
        parent: Any,
        anon_map: Any,
        bindparams: Any,
    ) -> Tuple[Any, ...]: ...
    def visit_dialect_options(
        self,
        attrname: Any,
        obj: Any,
        parent: Any,
        anon_map: Any,
        bindparams: Any,
    ) -> Tuple[Any, ...]: ...
    def visit_string_clauseelement_dict(
        self,
        attrname: Any,
        obj: Any,
        parent: Any,
        anon_map: Any,
        bindparams: Any,
    ) -> Tuple[Any, ...]: ...
    def visit_string_multi_dict(
        self,
        attrname: Any,
        obj: Any,
        parent: Any,
        anon_map: Any,
        bindparams: Any,
    ) -> Tuple[Any, ...]: ...
    def visit_fromclause_canonical_column_collection(
        self,
        attrname: Any,
        obj: Any,
        parent: Any,
        anon_map: Any,
        bindparams: Any,
    ) -> Tuple[Any, ...]: ...
    def visit_unknown_structure(
        self,
        attrname: Any,
        obj: Any,
        parent: Any,
        anon_map: Any,
        bindparams: Any,
    ) -> Tuple[Any, ...]: ...
    def visit_dml_ordered_values(
        self,
        attrname: Any,
        obj: Any,
        parent: Any,
        anon_map: Any,
        bindparams: Any,
    ) -> Tuple[Any, ...]: ...
    def visit_dml_values(
        self,
        attrname: Any,
        obj: Any,
        parent: Any,
        anon_map: Any,
        bindparams: Any,
    ) -> Tuple[Any, ...]: ...
    def visit_dml_multi_values(
        self,
        attrname: Any,
        obj: Any,
        parent: Any,
        anon_map: Any,
        bindparams: Any,
    ) -> Tuple[Any, ...]: ...

class HasCopyInternals: ...

class _CopyInternals(InternalTraversal):
    def visit_clauseelement(
        self,
        attrname: Any,
        parent: Any,
        element: _CE,
        clone: Any = ...,
        **kw: Any,
    ) -> _CE: ...
    def visit_clauseelement_list(
        self,
        attrname: Any,
        parent: Any,
        element: Iterable[_CE],
        clone: Any = ...,
        **kw: Any,
    ) -> List[_CE]: ...
    def visit_clauseelement_tuple(
        self,
        attrname: Any,
        parent: Any,
        element: Iterable[_CE],
        clone: Any = ...,
        **kw: Any,
    ) -> Tuple[_CE, ...]: ...
    def visit_executable_options(
        self,
        attrname: Any,
        parent: Any,
        element: Iterable[_CE],
        clone: Any = ...,
        **kw: Any,
    ) -> Tuple[_CE, ...]: ...
    def visit_clauseelement_unordered_set(
        self,
        attrname: Any,
        parent: Any,
        element: Iterable[_CE],
        clone: Any = ...,
        **kw: Any,
    ) -> Set[_CE]: ...
    def visit_clauseelement_tuples(
        self,
        attrname: Any,
        parent: Any,
        element: Iterable[Iterable[_CE]],
        clone: Any = ...,
        **kw: Any,
    ) -> List[Tuple[_CE, ...]]: ...
    def visit_string_clauseelement_dict(
        self,
        attrname: Any,
        parent: Any,
        element: Mapping[_T, _CE],
        clone: Any = ...,
        **kw: Any,
    ) -> Dict[_T, _CE]: ...
    def visit_setup_join_tuple(
        self,
        attrname: Any,
        parent: Any,
        element: Iterable[Tuple[Any, Any, Any, Any]],
        clone: Any = ...,
        **kw: Any,
    ) -> Tuple[Tuple[Any, Any, Any, Any]]: ...
    def visit_dml_ordered_values(
        self,
        attrname: Any,
        parent: Any,
        element: Iterable[Tuple[Any, Any]],
        clone: Any = ...,
        **kw: Any,
    ) -> List[Tuple[Any, Any]]: ...
    def visit_dml_values(
        self,
        attrname: Any,
        parent: Any,
        element: Mapping[Any, Any],
        clone: Any = ...,
        **kw: Any,
    ) -> Dict[Any, Any]: ...
    def visit_dml_multi_values(
        self,
        attrname: Any,
        parent: Any,
        element: Iterable[Iterable[Any]],
        clone: Any = ...,
        **kw: Any,
    ) -> List[List[Union[List[Any], Dict[Any, Any], bool]]]: ...
    def visit_propagate_attrs(
        self,
        attrname: Any,
        parent: Any,
        element: _T,
        clone: Any = ...,
        **kw: Any,
    ) -> _T: ...

class _GetChildren(InternalTraversal):
    def visit_has_cache_key(
        self, element: Any, **kw: Any
    ) -> Tuple[Any, ...]: ...
    def visit_clauseelement(self, element: _CE, **kw: Any) -> Tuple[_CE]: ...
    def visit_clauseelement_list(self, element: _T, **kw: Any) -> _T: ...
    def visit_clauseelement_tuple(self, element: _T, **kw: Any) -> _T: ...
    def visit_clauseelement_tuples(
        self, element: Iterable[Iterable[_CE]], **kw: Any
    ) -> Iterator[_CE]: ...
    def visit_fromclause_canonical_column_collection(
        self, element: Any, **kw: Any
    ) -> Tuple[Any, ...]: ...
    def visit_string_clauseelement_dict(
        self, element: Mapping[Any, _CE], **kw: Any
    ) -> Iterable[_CE]: ...
    def visit_fromclause_ordered_set(self, element: _T, **kw: Any) -> _T: ...
    def visit_clauseelement_unordered_set(
        self, element: _CE, **kw: Any
    ) -> _CE: ...
    def visit_setup_join_tuple(
        self, element: Iterable[Tuple[Any, Any, Any, Any]], **kw: Any
    ) -> Iterator[Any]: ...
    def visit_dml_ordered_values(
        self, element: Iterable[Tuple[Any, Any]], **kw: Any
    ) -> Iterator[Any]: ...
    def visit_dml_values(
        self, element: Mapping[Any, Any], **kw: Any
    ) -> Iterator[Any]: ...
    def visit_dml_multi_values(
        self, element: Any, **kw: Any
    ) -> Tuple[Any, ...]: ...
    def visit_propagate_attrs(
        self, element: Any, **kw: Any
    ) -> Tuple[Any, ...]: ...

class anon_map(Dict[_KT, _VT]):
    index: int = ...
    def __init__(self) -> None: ...
    def __missing__(self, key: Any) -> str: ...

class TraversalComparatorStrategy(InternalTraversal, util.MemoizedSlots):
    stack: Deque[Tuple[Any, Any]] = ...
    cache: Set[Tuple[Any, Any]] = ...
    def __init__(self) -> None: ...
    def compare(self, obj1: Any, obj2: Any, **kw: Any) -> bool: ...
    def compare_inner(self, obj1: Any, obj2: Any, **kw: Any) -> bool: ...
    def visit_has_cache_key(
        self,
        attrname: Any,
        left_parent: Any,
        left: Any,
        right_parent: Any,
        right: Any,
        **kw: Any,
    ) -> Optional[int]: ...
    def visit_propagate_attrs(
        self,
        attrname: Any,
        left_parent: Any,
        left: Any,
        right_parent: Any,
        right: Any,
        **kw: Any,
    ) -> bool: ...
    def visit_has_cache_key_list(
        self,
        attrname: Any,
        left_parent: Any,
        left: Any,
        right_parent: Any,
        right: Any,
        **kw: Any,
    ) -> Optional[int]: ...
    def visit_executable_options(
        self,
        attrname: Any,
        left_parent: Any,
        left: Any,
        right_parent: Any,
        right: Any,
        **kw: Any,
    ) -> Optional[int]: ...
    def visit_clauseelement(
        self,
        attrname: Any,
        left_parent: Any,
        left: Any,
        right_parent: Any,
        right: Any,
        **kw: Any,
    ) -> None: ...
    def visit_fromclause_canonical_column_collection(
        self,
        attrname: Any,
        left_parent: Any,
        left: Any,
        right_parent: Any,
        right: Any,
        **kw: Any,
    ) -> None: ...
    def visit_fromclause_derived_column_collection(
        self,
        attrname: Any,
        left_parent: Any,
        left: Any,
        right_parent: Any,
        right: Any,
        **kw: Any,
    ) -> None: ...
    def visit_string_clauseelement_dict(
        self,
        attrname: Any,
        left_parent: Any,
        left: Any,
        right_parent: Any,
        right: Any,
        **kw: Any,
    ) -> Optional[int]: ...
    def visit_clauseelement_tuples(
        self,
        attrname: Any,
        left_parent: Any,
        left: Any,
        right_parent: Any,
        right: Any,
        **kw: Any,
    ) -> Optional[int]: ...
    def visit_clauseelement_list(
        self,
        attrname: Any,
        left_parent: Any,
        left: Any,
        right_parent: Any,
        right: Any,
        **kw: Any,
    ) -> None: ...
    def visit_clauseelement_tuple(
        self,
        attrname: Any,
        left_parent: Any,
        left: Any,
        right_parent: Any,
        right: Any,
        **kw: Any,
    ) -> None: ...
    def visit_clauseelement_unordered_set(
        self,
        attrname: Any,
        left_parent: Any,
        left: Any,
        right_parent: Any,
        right: Any,
        **kw: Any,
    ) -> bool: ...
    def visit_fromclause_ordered_set(
        self,
        attrname: Any,
        left_parent: Any,
        left: Any,
        right_parent: Any,
        right: Any,
        **kw: Any,
    ) -> None: ...
    def visit_string(
        self,
        attrname: Any,
        left_parent: Any,
        left: Any,
        right_parent: Any,
        right: Any,
        **kw: Any,
    ) -> bool: ...
    def visit_string_list(
        self,
        attrname: Any,
        left_parent: Any,
        left: Any,
        right_parent: Any,
        right: Any,
        **kw: Any,
    ) -> bool: ...
    def visit_anon_name(
        self,
        attrname: Any,
        left_parent: Any,
        left: Any,
        right_parent: Any,
        right: Any,
        **kw: Any,
    ) -> bool: ...
    def visit_boolean(
        self,
        attrname: Any,
        left_parent: Any,
        left: Any,
        right_parent: Any,
        right: Any,
        **kw: Any,
    ) -> bool: ...
    def visit_operator(
        self,
        attrname: Any,
        left_parent: Any,
        left: Any,
        right_parent: Any,
        right: Any,
        **kw: Any,
    ) -> bool: ...
    def visit_type(
        self,
        attrname: Any,
        left_parent: Any,
        left: Any,
        right_parent: Any,
        right: Any,
        **kw: Any,
    ) -> bool: ...
    def visit_plain_dict(
        self,
        attrname: Any,
        left_parent: Any,
        left: Any,
        right_parent: Any,
        right: Any,
        **kw: Any,
    ) -> bool: ...
    def visit_dialect_options(
        self,
        attrname: Any,
        left_parent: Any,
        left: Any,
        right_parent: Any,
        right: Any,
        **kw: Any,
    ) -> bool: ...
    def visit_annotations_key(
        self,
        attrname: Any,
        left_parent: Any,
        left: Any,
        right_parent: Any,
        right: Any,
        **kw: Any,
    ) -> bool: ...
    def visit_plain_obj(
        self,
        attrname: Any,
        left_parent: Any,
        left: Any,
        right_parent: Any,
        right: Any,
        **kw: Any,
    ) -> bool: ...
    def visit_named_ddl_element(
        self,
        attrname: Any,
        left_parent: Any,
        left: Any,
        right_parent: Any,
        right: Any,
        **kw: Any,
    ) -> Union[int, bool]: ...
    def visit_prefix_sequence(
        self,
        attrname: Any,
        left_parent: Any,
        left: Any,
        right_parent: Any,
        right: Any,
        **kw: Any,
    ) -> Optional[int]: ...
    def visit_setup_join_tuple(
        self,
        attrname: Any,
        left_parent: Any,
        left: Any,
        right_parent: Any,
        right: Any,
        **kw: Any,
    ) -> Optional[int]: ...
    def visit_table_hint_list(
        self,
        attrname: Any,
        left_parent: Any,
        left: Any,
        right_parent: Any,
        right: Any,
        **kw: Any,
    ) -> Optional[int]: ...
    def visit_statement_hint_list(
        self,
        attrname: Any,
        left_parent: Any,
        left: Any,
        right_parent: Any,
        right: Any,
        **kw: Any,
    ) -> bool: ...
    def visit_unknown_structure(
        self,
        attrname: Any,
        left_parent: Any,
        left: Any,
        right_parent: Any,
        right: Any,
        **kw: Any,
    ) -> NoReturn: ...
    def visit_dml_ordered_values(
        self,
        attrname: Any,
        left_parent: Any,
        left: Any,
        right_parent: Any,
        right: Any,
        **kw: Any,
    ) -> Optional[int]: ...
    def visit_dml_values(
        self,
        attrname: Any,
        left_parent: Any,
        left: Any,
        right_parent: Any,
        right: Any,
        **kw: Any,
    ) -> Optional[int]: ...
    def visit_dml_multi_values(
        self,
        attrname: Any,
        left_parent: Any,
        left: Any,
        right_parent: Any,
        right: Any,
        **kw: Any,
    ) -> Optional[int]: ...
    def compare_clauselist(
        self, left: Any, right: Any, **kw: Any
    ) -> Union[List[str], int]: ...
    def compare_binary(
        self, left: Any, right: Any, **kw: Any
    ) -> Union[List[str], int]: ...
    def compare_bindparam(
        self, left: Any, right: Any, **kw: Any
    ) -> List[str]: ...

class ColIdentityComparatorStrategy(TraversalComparatorStrategy):
    def compare_column_element(
        self,
        left: Any,
        right: Any,
        use_proxies: bool = ...,
        equivalents: Any = ...,
        **kw: Any,
    ) -> int: ...
    def compare_column(self, left: Any, right: Any, **kw: Any) -> int: ...
    def compare_label(self, left: Any, right: Any, **kw: Any) -> int: ...
    def compare_table(self, left: Any, right: Any, **kw: Any) -> int: ...

from typing import Any
from typing import Iterable
from typing import Iterator
from typing import List
from typing import Set
from typing import Tuple
from typing import TypeVar

_T = TypeVar("_T")

def sort_as_subsets(
    tuples: Iterable[Tuple[Any, _T]], allitems: Iterable[_T]
) -> Iterator[List[_T]]: ...
def sort(
    tuples: Iterable[Tuple[Any, _T]],
    allitems: Iterable[_T],
    deterministic_order: bool = ...,
) -> Iterator[_T]: ...
def find_cycles(
    tuples: Iterable[Tuple[Any, ...]], allitems: Iterable[Any]
) -> Set[Any]: ...

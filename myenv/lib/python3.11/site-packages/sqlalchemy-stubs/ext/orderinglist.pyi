from typing import Callable
from typing import List
from typing import Optional
from typing import Sequence
from typing import TypeVar

_T = TypeVar("_T")
OrderingFunc = Callable[[int, Sequence[_T]], int]

def ordering_list(
    attr: str,
    count_from: Optional[int] = ...,
    ordering_func: Optional[OrderingFunc] = ...,
    reorder_on_append: bool = ...,
) -> Callable[[], OrderingList]: ...

class OrderingList(List[_T]):
    ordering_attr: str = ...
    ordering_func: OrderingFunc = ...
    reorder_on_append: bool = ...
    def __init__(
        self,
        ordering_attr: Optional[str] = ...,
        ordering_func: Optional[OrderingFunc] = ...,
        reorder_on_append: bool = ...,
    ) -> None: ...
    def reorder(self) -> None: ...

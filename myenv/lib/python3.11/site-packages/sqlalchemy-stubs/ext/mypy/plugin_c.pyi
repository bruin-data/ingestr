from typing import Any

from mypy.plugin import Callable as Callable
from mypy.plugin import FunctionContext as FunctionContext
from mypy.plugin import Optional as Optional
from mypy.plugin import Plugin
from mypy.plugin import Type as Type
from mypy.types import CallableType as CallableType

class CustomPlugin(Plugin):
    def get_function_hook(
        self, fullname: str
    ) -> Optional[Callable[[FunctionContext], Type]]: ...

def make_dec_thing(name: Any): ...

COLUMN_THINGY: int
RELATIONSHIP_THINGY: int

def plugin(version: str) -> Any: ...

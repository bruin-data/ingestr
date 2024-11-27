from typing import Any

from mypy.plugin import Callable as Callable
from mypy.plugin import ClassDefContext as ClassDefContext
from mypy.plugin import Optional as Optional
from mypy.plugin import Plugin

prefix: str

class CustomPlugin(Plugin):
    def get_class_decorator_hook(
        self, fullname: str
    ) -> Optional[Callable[[ClassDefContext], None]]: ...
    def get_customize_class_mro_hook(
        self, fullname: str
    ) -> Optional[Callable[[ClassDefContext], None]]: ...

def fill_in_decorators(ctx: ClassDefContext) -> None: ...
def cls_decorator_hook(ctx: ClassDefContext) -> None: ...

COLUMN_THINGY: int
RELATIONSHIP_THINGY: int

def plugin(version: str) -> Any: ...

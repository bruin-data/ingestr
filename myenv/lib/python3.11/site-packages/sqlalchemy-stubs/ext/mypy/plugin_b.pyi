from typing import Any

from mypy.plugin import Callable as Callable
from mypy.plugin import CallableType as CallableType
from mypy.plugin import ClassDefContext as ClassDefContext
from mypy.plugin import FunctionSigContext as FunctionSigContext
from mypy.plugin import Optional as Optional
from mypy.plugin import Plugin

cls_decorator_names_we_care_about: Any
classes_w_clsdec_methods: Any

class CustomPlugin(Plugin):
    def get_function_signature_hook(
        self, fullname: str
    ) -> Optional[Callable[[FunctionSigContext], CallableType]]: ...
    def get_class_decorator_hook(
        self, fullname: str
    ) -> Optional[Callable[[ClassDefContext], None]]: ...
    def get_customize_class_mro_hook(
        self, fullname: str
    ) -> Optional[Callable[[ClassDefContext], None]]: ...

def fill_in_decorators(ctx: ClassDefContext) -> None: ...
def cls_decorator_hook(ctx: ClassDefContext) -> None: ...
def plugin(version: str) -> Any: ...

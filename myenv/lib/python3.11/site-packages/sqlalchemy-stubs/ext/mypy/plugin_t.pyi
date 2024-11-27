from typing import Any

from mypy.plugin import AttributeContext as AttributeContext
from mypy.plugin import Callable as Callable
from mypy.plugin import CallableType as CallableType
from mypy.plugin import ClassDefContext as ClassDefContext
from mypy.plugin import DynamicClassDefContext as DynamicClassDefContext
from mypy.plugin import FunctionContext as FunctionContext
from mypy.plugin import FunctionSigContext as FunctionSigContext
from mypy.plugin import List as List
from mypy.plugin import MethodContext as MethodContext
from mypy.plugin import MethodSigContext as MethodSigContext
from mypy.plugin import MypyFile as MypyFile
from mypy.plugin import Optional as Optional
from mypy.plugin import Plugin
from mypy.plugin import Tuple as Tuple
from mypy.plugin import Type as Type

class CustomPlugin(Plugin):
    def get_type_analyze_hook(self, fullname: str) -> Any: ...
    def get_additional_deps(
        self, file: MypyFile
    ) -> List[Tuple[int, str, int]]: ...
    def get_function_signature_hook(
        self, fullname: str
    ) -> Optional[Callable[[FunctionSigContext], CallableType]]: ...
    def get_function_hook(
        self, fullname: str
    ) -> Optional[Callable[[FunctionContext], Type]]: ...
    def get_method_signature_hook(
        self, fullname: str
    ) -> Optional[Callable[[MethodSigContext], CallableType]]: ...
    def get_method_hook(
        self, fullname: str
    ) -> Optional[Callable[[MethodContext], Type]]: ...
    def get_attribute_hook(
        self, fullname: str
    ) -> Optional[Callable[[AttributeContext], Type]]: ...
    def get_class_decorator_hook(
        self, fullname: str
    ) -> Optional[Callable[[ClassDefContext], None]]: ...
    def get_metaclass_hook(
        self, fullname: str
    ) -> Optional[Callable[[ClassDefContext], None]]: ...
    def get_base_class_hook(
        self, fullname: str
    ) -> Optional[Callable[[ClassDefContext], None]]: ...
    def get_customize_class_mro_hook(
        self, fullname: str
    ) -> Optional[Callable[[ClassDefContext], None]]: ...
    def get_dynamic_class_hook(
        self, fullname: str
    ) -> Optional[Callable[[DynamicClassDefContext], None]]: ...

def debug(ctx: Any) -> None: ...
def plugin(version: str) -> Any: ...

from typing import Any
from typing import overload
from typing import Type

from . import exc as exc
from . import util as util
from .engine.base import Connection
from .engine.base import Engine
from .engine.reflection import Inspector
from .orm import InstanceState
from .orm import Mapper

# this doesn't really seem to work...
@overload
def inspect(subject: Engine, raiseerr: bool = ...) -> Inspector: ...
@overload
def inspect(subject: Connection, raiseerr: bool = ...) -> Inspector: ...
@overload
def inspect(subject: Type, raiseerr: bool = ...) -> Mapper: ...
@overload
def inspect(subject: object, raiseerr: bool = ...) -> InstanceState: ...
@overload
def inspect(subject: Any, raiseerr: bool = ...) -> Any: ...

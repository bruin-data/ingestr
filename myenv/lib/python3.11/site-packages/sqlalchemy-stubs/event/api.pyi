from typing import Any

from .. import exc as exc
from .. import util as util

CANCEL: Any
NO_RETVAL: Any

def listen(
    target: Any, identifier: Any, fn: Any, *args: Any, **kw: Any
) -> None: ...
def listens_for(target: Any, identifier: Any, *args: Any, **kw: Any): ...
def remove(target: Any, identifier: Any, fn: Any) -> None: ...
def contains(target: Any, identifier: Any, fn: Any): ...

from typing import Any
from typing import Optional

def Serializer(*args: Any, **kw: Any): ...
def Deserializer(
    file: Any,
    metadata: Optional[Any] = ...,
    scoped_session: Optional[Any] = ...,
    engine: Optional[Any] = ...,
): ...
def dumps(obj: Any, protocol: Any = ...): ...
def loads(
    data: Any,
    metadata: Optional[Any] = ...,
    scoped_session: Optional[Any] = ...,
    engine: Optional[Any] = ...,
): ...

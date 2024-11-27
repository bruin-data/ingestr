from typing import Any
from typing import Optional

from .. import exc as sa_exc
from .. import util as util
from ..exc import MultipleResultsFound as MultipleResultsFound
from ..exc import NoResultFound as NoResultFound

NO_STATE: Any

class StaleDataError(sa_exc.SQLAlchemyError): ...

ConcurrentModificationError = StaleDataError

class FlushError(sa_exc.SQLAlchemyError): ...
class UnmappedError(sa_exc.InvalidRequestError): ...
class ObjectDereferencedError(sa_exc.SQLAlchemyError): ...

class DetachedInstanceError(sa_exc.SQLAlchemyError):
    code: str = ...

class UnmappedInstanceError(UnmappedError):
    def __init__(self, obj: Any, msg: Optional[Any] = ...) -> None: ...
    def __reduce__(self): ...

class UnmappedClassError(UnmappedError):
    def __init__(self, cls: Any, msg: Optional[Any] = ...) -> None: ...
    def __reduce__(self): ...

class ObjectDeletedError(sa_exc.InvalidRequestError):
    def __init__(self, state: Any, msg: Optional[Any] = ...) -> None: ...
    def __reduce__(self): ...

class UnmappedColumnError(sa_exc.InvalidRequestError): ...

class LoaderStrategyException(sa_exc.InvalidRequestError):
    def __init__(
        self,
        applied_to_property_type: Any,
        requesting_property: Any,
        applies_to: Any,
        actual_strategy_type: Any,
        strategy_key: Any,
    ) -> None: ...

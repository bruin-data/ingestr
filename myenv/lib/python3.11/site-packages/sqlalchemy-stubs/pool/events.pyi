from typing import Any

from .base import Pool as Pool
from .. import event as event
from ..engine.base import Engine as Engine

class PoolEvents(event.Events):
    def connect(
        self, dbapi_connection: Any, connection_record: Any
    ) -> None: ...
    def first_connect(
        self, dbapi_connection: Any, connection_record: Any
    ) -> None: ...
    def checkout(
        self,
        dbapi_connection: Any,
        connection_record: Any,
        connection_proxy: Any,
    ) -> None: ...
    def checkin(
        self, dbapi_connection: Any, connection_record: Any
    ) -> None: ...
    def reset(self, dbapi_connection: Any, connection_record: Any) -> None: ...
    def invalidate(
        self, dbapi_connection: Any, connection_record: Any, exception: Any
    ) -> None: ...
    def soft_invalidate(
        self, dbapi_connection: Any, connection_record: Any, exception: Any
    ) -> None: ...
    def close(self, dbapi_connection: Any, connection_record: Any) -> None: ...
    def detach(
        self, dbapi_connection: Any, connection_record: Any
    ) -> None: ...
    def close_detached(self, dbapi_connection: Any) -> None: ...

import abc
from typing import Any

from .interfaces import TypingDBAPIConnection as _DBAPIConnection
from .interfaces import Dialect
from ..util import ABC

class ConnectionCharacteristic(ABC, metaclass=abc.ABCMeta):
    transactional: bool = ...
    @abc.abstractmethod
    def reset_characteristic(
        self, dialect: Dialect, dbapi_conn: _DBAPIConnection
    ) -> None: ...
    @abc.abstractmethod
    def set_characteristic(
        self, dialect: Dialect, dbapi_conn: _DBAPIConnection, value: Any
    ) -> None: ...
    @abc.abstractmethod
    def get_characteristic(
        self, dialect: Dialect, dbapi_conn: _DBAPIConnection
    ) -> Any: ...

class IsolationLevelCharacteristic(ConnectionCharacteristic):
    transactional: bool = ...
    def reset_characteristic(
        self, dialect: Dialect, dbapi_conn: _DBAPIConnection
    ) -> None: ...
    def set_characteristic(
        self, dialect: Dialect, dbapi_conn: _DBAPIConnection, value: Any
    ) -> None: ...
    def get_characteristic(
        self, dialect: Dialect, dbapi_conn: _DBAPIConnection
    ) -> Any: ...

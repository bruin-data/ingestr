from typing import Type

from .mock import MockConnection as _MockConnection

class MockEngineStrategy:
    MockConnection: Type[_MockConnection] = ...

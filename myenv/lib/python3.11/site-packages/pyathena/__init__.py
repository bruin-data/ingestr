# -*- coding: utf-8 -*-
from __future__ import annotations

import datetime
from typing import TYPE_CHECKING, Any, FrozenSet, Type, overload

from pyathena.error import *  # noqa

if TYPE_CHECKING:
    from pyathena.connection import Connection, ConnectionCursor
    from pyathena.cursor import Cursor

__version__ = "3.9.0"
user_agent_extra: str = f"PyAthena/{__version__}"

# Globals https://www.python.org/dev/peps/pep-0249/#globals
apilevel: str = "2.0"
threadsafety: int = 2
paramstyle: str = "pyformat"


class DBAPITypeObject(FrozenSet[str]):
    """Type Objects and Constructors

    https://www.python.org/dev/peps/pep-0249/#type-objects-and-constructors
    """

    def __eq__(self, other: object):
        if isinstance(other, frozenset):
            return frozenset.__eq__(self, other)
        else:
            return other in self

    def __ne__(self, other: object):
        if isinstance(other, frozenset):
            return frozenset.__ne__(self, other)
        else:
            return other not in self

    def __hash__(self):
        return frozenset.__hash__(self)


# https://docs.aws.amazon.com/athena/latest/ug/data-types.html
STRING: DBAPITypeObject = DBAPITypeObject(("char", "varchar", "map", "array", "row"))
BINARY: DBAPITypeObject = DBAPITypeObject(("varbinary",))
BOOLEAN: DBAPITypeObject = DBAPITypeObject(("boolean",))
NUMBER: DBAPITypeObject = DBAPITypeObject(
    ("tinyint", "smallint", "bigint", "integer", "real", "double", "float", "decimal")
)
DATE: DBAPITypeObject = DBAPITypeObject(("date",))
TIME: DBAPITypeObject = DBAPITypeObject(("time", "time with time zone"))
DATETIME: DBAPITypeObject = DBAPITypeObject(("timestamp", "timestamp with time zone"))
JSON: DBAPITypeObject = DBAPITypeObject(("json",))

Date: Type[datetime.date] = datetime.date
Time: Type[datetime.time] = datetime.time
Timestamp: Type[datetime.datetime] = datetime.datetime


@overload
def connect(*args, cursor_class: None = ..., **kwargs) -> "Connection[Cursor]":
    ...


@overload
def connect(
    *args, cursor_class: Type[ConnectionCursor], **kwargs
) -> "Connection[ConnectionCursor]":
    ...


def connect(*args, **kwargs) -> "Connection[Any]":
    from pyathena.connection import Connection

    return Connection(*args, **kwargs)

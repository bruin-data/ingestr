# -*- coding: utf-8 -*-
__all__ = [
    "Error",
    "Warning",
    "InterfaceError",
    "DatabaseError",
    "InternalError",
    "OperationalError",
    "ProgrammingError",
    "DataError",
    "NotSupportedError",
]


class Error(Exception):
    pass


class Warning(Exception):  # noqa: N818
    pass


class InterfaceError(Error):
    pass


class DatabaseError(Error):
    pass


class InternalError(DatabaseError):
    pass


class OperationalError(DatabaseError):
    pass


class ProgrammingError(DatabaseError):
    pass


class IntegrityError(DatabaseError):
    pass


class DataError(DatabaseError):
    pass


class NotSupportedError(DatabaseError):
    pass

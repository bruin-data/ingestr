from datetime import date
from datetime import datetime as Datetime
from datetime import time
from time import localtime


def Date(year: int, month: int, day: int) -> date:
    """Constuct an object holding a date value.

    This function is part of the `DBAPI 2.0 specification
    <http://www.python.org/dev/peps/pep-0249/>`_.

    :rtype: :class:`datetime.date`
    """
    return date(year, month, day)


def Time(hour: int, minute: int, second: int) -> time:
    """Construct an object holding a time value.

    This function is part of the `DBAPI 2.0 specification
    <http://www.python.org/dev/peps/pep-0249/>`_.

    :rtype: :class:`datetime.time`
    """
    return time(hour, minute, second)


def Timestamp(year: int, month: int, day: int, hour: int, minute: int, second: int) -> Datetime:
    """Construct an object holding a timestamp value.

    This function is part of the `DBAPI 2.0 specification
    <http://www.python.org/dev/peps/pep-0249/>`_.

    :rtype: :class:`datetime.datetime`
    """
    return Datetime(year, month, day, hour, minute, second)


def DateFromTicks(ticks: float) -> date:
    """Construct an object holding a date value from the given ticks value
    (number of seconds since the epoch).

    This function is part of the `DBAPI 2.0 specification
    <http://www.python.org/dev/peps/pep-0249/>`_.

    :rtype: :class:`datetime.date`
    """
    return Date(*localtime(ticks)[:3])


def TimeFromTicks(ticks: float) -> time:
    """Construct an objet holding a time value from the given ticks value
    (number of seconds since the epoch).

    This function is part of the `DBAPI 2.0 specification
    <http://www.python.org/dev/peps/pep-0249/>`_.

    :rtype: :class:`datetime.time`
    """
    return Time(*localtime(ticks)[3:6])


def TimestampFromTicks(ticks: float) -> Datetime:
    """Construct an object holding a timestamp value from the given ticks value
    (number of seconds since the epoch).

    This function is part of the `DBAPI 2.0 specification
    <http://www.python.org/dev/peps/pep-0249/>`_.

    :rtype: :class:`datetime.datetime`
    """
    return Timestamp(*localtime(ticks)[:6])


def Binary(value: bytes):
    """Construct an object holding binary data.

    This function is part of the `DBAPI 2.0 specification
    <http://www.python.org/dev/peps/pep-0249/>`_.

    """
    return value

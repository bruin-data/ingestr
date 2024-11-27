import typing
from datetime import timedelta as Timedelta

from redshift_connector.config import max_int4, max_int8, min_int4, min_int8


class Interval:
    """An Interval represents a measurement of time.  In Amazon Redshift, an
    interval is defined in the measure of months, days, and microseconds; as
    such, the interval type represents the same information.

    Note that values of the :attr:`microseconds`, :attr:`days` and
    :attr:`months` properties are independently measured and cannot be
    converted to each other.  A month may be 28, 29, 30, or 31 days, and a day
    may occasionally be lengthened slightly by a leap second.

    .. attribute:: microseconds

        Measure of microseconds in the interval.

        The microseconds value is constrained to fit into a signed 64-bit
        integer.  Any attempt to set a value too large or too small will result
        in an OverflowError being raised.

    .. attribute:: days

        Measure of days in the interval.

        The days value is constrained to fit into a signed 32-bit integer.
        Any attempt to set a value too large or too small will result in an
        OverflowError being raised.

    .. attribute:: months

        Measure of months in the interval.

        The months value is constrained to fit into a signed 32-bit integer.
        Any attempt to set a value too large or too small will result in an
        OverflowError being raised.
    """

    def __init__(self: "Interval", microseconds: int = 0, days: int = 0, months: int = 0) -> None:
        self.microseconds = microseconds
        self.days = days
        self.months = months

    def _setMicroseconds(self: "Interval", value: int) -> None:
        if not isinstance(value, int):
            raise TypeError("microseconds must be an integer type")
        elif not (min_int8 <= value < max_int8):
            raise OverflowError("microseconds must be representable as a 64-bit integer")
        else:
            self._microseconds = value

    def _setDays(self: "Interval", value: int) -> None:
        if not isinstance(value, int):
            raise TypeError("days must be an integer type")
        elif not (min_int4 <= value < max_int4):
            raise OverflowError("days must be representable as a 32-bit integer")
        else:
            self._days = value

    def _setMonths(self: "Interval", value: int) -> None:
        if not isinstance(value, int):
            raise TypeError("months must be an integer type")
        elif not (min_int4 <= value < max_int4):
            raise OverflowError("months must be representable as a 32-bit integer")
        else:
            self._months = value

    microseconds = property(lambda self: self._microseconds, _setMicroseconds)
    days = property(lambda self: self._days, _setDays)
    months = property(lambda self: self._months, _setMonths)

    def __repr__(self: "Interval") -> str:
        return "<Interval %s months %s days %s microseconds>" % (self.months, self.days, self.microseconds)

    def __eq__(self: "Interval", other: object) -> bool:
        return (
            other is not None
            and isinstance(other, Interval)
            and self.months == other.months
            and self.days == other.days
            and self.microseconds == other.microseconds
        )

    def __neq__(self: "Interval", other: "Interval") -> bool:
        return not self.__eq__(other)

    def total_seconds(self: "Interval") -> float:
        """Total seconds in the Interval, excluding month field."""
        return ((self.days * 86400) * 10**6 + self.microseconds) / 10**6


class IntervalYearToMonth(Interval):
    """An Interval Year To Month represents a measurement of time of the order
    of a few months and years. Note the difference with Interval which can
    represent any length of time. Since this class only represents an interval
    of the order of months, we just use the :attr:`months`, the other inherited
    attributes must be set to 0 at all times.

    Note that 1year = 12months.
    """

    def __init__(
        self: "IntervalYearToMonth", months: int = 0, year_month: typing.Optional[typing.Tuple[int, int]] = None
    ) -> None:
        if year_month is not None:
            year, month = year_month
            self.months = year * 12 + month
        else:
            self.months = months

    def _setMicroseconds(self: "IntervalYearToMonth", value: int) -> None:
        raise ValueError("microseconds cannot be set for an Interval Year To Month object")

    def _setDays(self: "IntervalYearToMonth", value: int) -> None:
        raise ValueError("days cannot be set for an Interval Year To Month object")

    def _setMonths(self: "IntervalYearToMonth", value: int) -> None:
        return super(IntervalYearToMonth, self)._setMonths(value)

    # microseconds = property(lambda self: self._microseconds, _setMicroseconds)
    # days = property(lambda self: self._days, _setDays)
    months = property(lambda self: self._months, _setMonths)

    def getYearMonth(self: "IntervalYearToMonth") -> typing.Tuple[int, int]:
        years = int(self.months / 12)
        months = self.months - 12 * years
        return (years, months)

    def __repr__(self: "IntervalYearToMonth") -> str:
        return "<IntervalYearToMonth %s months>" % (self.months)

    def __eq__(self: "IntervalYearToMonth", other: object) -> bool:
        return other is not None and isinstance(other, IntervalYearToMonth) and self.months == other.months

    def __neq__(self: "IntervalYearToMonth", other: "Interval") -> bool:
        return not self.__eq__(other)


class IntervalDayToSecond(Interval):
    """An Interval Day To Second represents a measurement of time of the order
    of a few microseconds. Note the difference with Interval which can
    represent any length of time. Since this class only represents an interval
    of the order of microsecodns, we just use the :attr:`microseconds`, the other
    inherited attributes must be set to 0 at all times.

    Note that 1day = 24 * 3600 * 1000000 microseconds.
    """

    def __init__(
        self: "IntervalDayToSecond", microseconds: int = 0, timedelta: typing.Optional[Timedelta] = None
    ) -> None:
        if timedelta is not None:
            self.microseconds = int(timedelta.total_seconds() * (10**6))
        else:
            self.microseconds = microseconds

    def _setMicroseconds(self: "IntervalDayToSecond", value: int) -> None:
        return super(IntervalDayToSecond, self)._setMicroseconds(value)

    def _setDays(self: "IntervalDayToSecond", value: int) -> None:
        raise ValueError("days cannot be set for an Interval Day To Second object")

    def _setMonths(self: "IntervalDayToSecond", value: int) -> None:
        raise ValueError("months cannot be set for an Interval Day To Second object")

    microseconds = property(lambda self: self._microseconds, _setMicroseconds)
    # days = property(lambda self: self._days, _setDays)
    # months = property(lambda self: self._months, _setMonths)

    def __repr__(self: "IntervalDayToSecond") -> str:
        return "<IntervalDayToSecond %s microseconds>" % (self.microseconds)

    def __eq__(self: "IntervalDayToSecond", other: object) -> bool:
        return other is not None and isinstance(other, IntervalDayToSecond) and self.microseconds == other.microseconds

    def __neq__(self: "IntervalDayToSecond", other: "Interval") -> bool:
        return not self.__eq__(other)

    def getTimedelta(self: "IntervalDayToSecond") -> Timedelta:
        return Timedelta(microseconds=self.microseconds)

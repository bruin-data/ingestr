"""Zendesk source helpers"""

from typing import List, Tuple

from dlt.common import pendulum
from dlt.common.time import timedelta


def make_date_ranges(
    start: pendulum.DateTime, end: pendulum.DateTime, step: timedelta
) -> List[Tuple[pendulum.DateTime, pendulum.DateTime]]:
    """Make tuples of (start, end) date ranges between the given `start` and `end` dates.
    The last range in the resulting list will be capped to the value of `end` argument so it may be smaller than `step`

    Example usage, create 1 week ranges between January 1st 2023 and today:
    >>> make_date_ranges(pendulum.DateTime(2023, 1, 1).as_tz('UTC'), pendulum.today(), timedelta(weeks=1))
    """
    ranges = []
    while True:
        end_time = min(start + step, end)
        ranges.append((start, end_time))
        if end_time == end:
            break
        start = end_time
    return ranges

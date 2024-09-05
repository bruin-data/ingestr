from typing import List

import pendulum


def split_date_range(
    start_date: pendulum.DateTime, end_date: pendulum.DateTime
) -> List[tuple]:
    interval = "days"
    if (end_date - start_date).days <= 1:
        interval = "hours"

    intervals = []
    current = start_date
    while current < end_date:
        next_date = min(current.add(**{interval: 1}), end_date)
        intervals.append((current.isoformat(), next_date.isoformat()))
        current = next_date
    return intervals

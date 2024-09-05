import pendulum

from ingestr.src.klaviyo.helpers import split_date_range


def test_split_date_range():
    start_date = pendulum.datetime(2024, 1, 1)
    end_date = pendulum.datetime(2024, 1, 5)
    intervals = split_date_range(start_date, end_date)
    assert len(intervals) == 4
    assert intervals[0] == (start_date.isoformat(), start_date.add(days=1).isoformat())
    assert intervals[1] == (
        start_date.add(days=1).isoformat(),
        start_date.add(days=2).isoformat(),
    )
    assert intervals[2] == (
        start_date.add(days=2).isoformat(),
        start_date.add(days=3).isoformat(),
    )
    assert intervals[3] == (
        start_date.add(days=3).isoformat(),
        start_date.add(days=4).isoformat(),
    )


def test_split_date_range_with_hours():
    start_date = pendulum.datetime(2024, 1, 1, 12)
    end_date = pendulum.datetime(2024, 1, 1, 15)
    intervals = split_date_range(start_date, end_date)
    assert len(intervals) == 3
    assert intervals[0] == (start_date.isoformat(), start_date.add(hours=1).isoformat())
    assert intervals[1] == (
        start_date.add(hours=1).isoformat(),
        start_date.add(hours=2).isoformat(),
    )
    assert intervals[2] == (start_date.add(hours=2).isoformat(), end_date.isoformat())

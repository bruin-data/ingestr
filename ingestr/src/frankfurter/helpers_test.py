import pytest
from datetime import datetime
import pendulum
from ingestr.src.frankfurter.helpers import validate_dates

def test_interval_start_does_not_exceed_end_date():
    start_date = pendulum.datetime(2025, 4, 10)
    end_date = pendulum.datetime(2025, 4, 11)
    # Should not raise an exception
    validate_dates(start_date=start_date, end_date=end_date)

    with pytest.raises(ValueError, match="Interval-end cannot be before interval-start."):
        validate_dates(start_date=end_date, end_date=start_date)


def test_interval_start_can_equal_interval_end():
    start_date = pendulum.datetime(2025, 4, 10)
    end_date = pendulum.datetime(2025, 4, 10)
    # Should not raise an exception
    validate_dates(start_date=start_date, end_date=end_date)


def test_interval_start_does_not_exceed_current_date():
    start_date = pendulum.now().add(days=1)  # Future date
    end_date = pendulum.now()
    with pytest.raises(ValueError, match="Interval-start cannot be in the future."):
        validate_dates(start_date=start_date, end_date=end_date)


def test_interval_end_does_not_exceed_current_date():
    start_date = pendulum.now().subtract(days=1)
    end_date = pendulum.now().add(days=1)  # Future date
    with pytest.raises(ValueError, match="Interval-end cannot be in the future."):
        validate_dates(start_date=start_date, end_date=end_date)




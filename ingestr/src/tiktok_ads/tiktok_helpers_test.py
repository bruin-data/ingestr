import pendulum

from tiktok_ads import find_intervals
from tiktok_ads.tiktok_helpers import flat_structure


def test_flat_structure():
    items = [
        {
            "dimensions": {"ad_id": "123456789", "country_code": "DE"},
            "metrics": {
                "impressions": "0",
                "clicks": "20",
                "ctr": "0.00",
                "cpc": "0.00",
                "cpm": "0.00",
            },
        }
    ]

    expected_output = [
        {
            "ad_id": "123456789",
            "country_code": "DE",
            "impressions": "0",
            "clicks": "20",
            "ctr": "0.00",
            "cpc": "0.00",
            "cpm": "0.00",
        }
    ]

    assert flat_structure(items) == expected_output


def test_find_intervals():
    current_date = pendulum.datetime(2024, 10, 15, 5, 45, 0, tz="Asia/Kathmandu")
    end_date = pendulum.datetime(2024, 12, 19, 5, 45, 0, tz="Asia/Kathmandu")
    interval_days = 30

    expected_intervals = [
        (
            pendulum.datetime(2024, 10, 15, 5, 45, 0, tz="Asia/Kathmandu"),
            pendulum.datetime(2024, 11, 14, 5, 45, 0, tz="Asia/Kathmandu"),
        ),
        (
            pendulum.datetime(2024, 11, 15, 5, 45, 0, tz="Asia/Kathmandu"),
            pendulum.datetime(2024, 12, 15, 5, 45, 0, tz="Asia/Kathmandu"),
        ),
        (
            pendulum.datetime(2024, 12, 16, 5, 45, 0, tz="Asia/Kathmandu"),
            pendulum.datetime(2024, 12, 19, 5, 45, 0, tz="Asia/Kathmandu"),
        ),
    ]

    assert find_intervals(current_date, end_date, interval_days) == expected_intervals

from dlt.common.pendulum import pendulum

from .helpers import convert_datetime_fields, find_latest_timestamp_from_page


def test_convert_datetime_fields():
    item = {
        "key1": "val1",
        "created_datetime": "2024-06-20T07:39:36.514848+00:00",
        "sent_datetime": "2024-06-20T07:40:20.166593+00:00",
        "should_send_datetime": "2024-06-20T07:39:37.514848+00:00",
    }

    actual = convert_datetime_fields(item)

    assert actual == {
        "key1": "val1",
        "created_datetime": pendulum.datetime(2024, 6, 20, 7, 39, 36, 514848, tz="UTC"),
        "sent_datetime": pendulum.datetime(2024, 6, 20, 7, 40, 20, 166593, tz="UTC"),
        "should_send_datetime": pendulum.datetime(
            2024, 6, 20, 7, 39, 37, 514848, tz="UTC"
        ),
        "updated_datetime": pendulum.datetime(2024, 6, 20, 7, 40, 20, 166593, tz="UTC"),
    }


def test_find_latest_timestamp_from_page():
    items = [
        {
            "key1": "val1",
            "created_datetime": "2024-06-20T07:39:36.514848+00:00",
            "sent_datetime": "2024-06-20T07:40:20.166593+00:00",
            "should_send_datetime": "2024-06-20T07:39:37.514848+00:00",
        },
        {
            "key1": "val2",
            "created_datetime": "2024-06-20T07:39:36.514848+00:00",
            "sent_datetime": "2024-06-20T07:40:21.123123+00:00",
            "should_send_datetime": "2024-06-20T07:39:37.514848+00:00",
        },
    ]

    actual = find_latest_timestamp_from_page(items)

    assert actual == pendulum.datetime(2024, 6, 20, 7, 40, 21, 123123, tz="UTC")

import pytest
from google.analytics.data_v1beta.types import MinuteRange

from ingestr.src.google_analytics.helpers import (
    convert_minutes_ranges_to_minute_range_objects,
    parse_google_analytics_uri,
)


def test_convert_minutes_ranges_to_minute_range_objects():
    user_input = "1-2,5-6"
    expected_result = [
        MinuteRange(name="1-2 minutes ago", start_minutes_ago=2),
        MinuteRange(name="5-6 minutes ago", start_minutes_ago=6),
    ]
    result = convert_minutes_ranges_to_minute_range_objects(user_input)
    assert result == expected_result

    user_input_2 = "1-2,5-6,6-8"
    expected_result_2 = "You can define up to two time minutes ranges, formatted as comma-separated values `0-5,25-29`"
    with pytest.raises(ValueError, match=expected_result_2):
        convert_minutes_ranges_to_minute_range_objects(user_input_2)

    user_input_3 = "12,56"
    expected_result_3 = "Invalid input. Minutes range should be startminute-endminute format. For example: 1-2,5-6"
    with pytest.raises(ValueError, match=expected_result_3):
        convert_minutes_ranges_to_minute_range_objects(user_input_3)

    user_input_4 = "1-2,5-"
    expected_result_4 = "Invalid input. Minutes range should be startminute-endminute format. For example: 1-2,5-6"
    with pytest.raises(ValueError, match=expected_result_4):
        convert_minutes_ranges_to_minute_range_objects(user_input_4)


def test_parse_google_analytics_uri():
    uri1 = "google_analytics://?credentials_base64=eyJrZXkiOiAidmFsdWUifQ==&property_id=1234567890"
    expected_result = {"credentials": {"key": "value"}, "property_id": "1234567890"}
    result = parse_google_analytics_uri(uri1)
    assert result == expected_result

    uri_2 = "google_analytics://?credentials_base64=eyJrZXkiOiAidmFsdWUifQ=="
    with pytest.raises(
        ValueError, match="property_id is required to connect to Google Analytics"
    ):
        parse_google_analytics_uri(uri_2)

    uri_3 = "google_analytics://?property_id=1234567890"
    with pytest.raises(
        ValueError,
        match="credentials_path or credentials_base64 is required to connect Google Analytics",
    ):
        parse_google_analytics_uri(uri_3)

    uri_4 = (
        "google_analytics://credentials_path=credentials.json&property_id=1234567890"
    )
    with pytest.raises(
        ValueError,
        match="credentials_path or credentials_base64 is required to connect Google Analytics",
    ):
        parse_google_analytics_uri(uri_4)

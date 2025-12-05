# Copyright 2022-2025 ScaleVector
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#   http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

import pytest
from google.analytics.data_v1beta.types import MinuteRange

from ingestr.src.google_analytics.helpers import (
    convert_minutes_ranges_to_minute_range_objects,
    parse_google_analytics_uri,
)


def test_convert_minutes_ranges_to_minute_range_objects():
    user_input = "1-2,5-6"
    expected_result = [
        MinuteRange(name="1-2 minutes ago", start_minutes_ago=2, end_minutes_ago=1),
        MinuteRange(name="5-6 minutes ago", start_minutes_ago=6, end_minutes_ago=5),
    ]
    result = convert_minutes_ranges_to_minute_range_objects(user_input)
    assert result == expected_result

    user_input_3 = "12,56"
    expected_result_3 = "Invalid input. Minutes range should be startminute-endminute format. For example: 1-2,5-6"
    with pytest.raises(ValueError, match=expected_result_3):
        convert_minutes_ranges_to_minute_range_objects(user_input_3)

    user_input_4 = "1-2,5-"
    expected_result_4 = "Invalid input '.*'\. Both start and end minutes must be digits. For example: 1-2,5-6"
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

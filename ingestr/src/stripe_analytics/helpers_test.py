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

"""Tests for stripe_analytics helpers module."""

from datetime import datetime

from .helpers import generate_date_ranges


class TestGenerateDateRanges:
    """Tests for generate_date_ranges function."""

    def test_single_hour(self):
        """Test generating a single hour range."""
        start_ts = int(datetime(2024, 1, 1, 0, 0, 0).timestamp())
        end_ts = int(datetime(2024, 1, 1, 1, 0, 0).timestamp())

        ranges = list(generate_date_ranges(start_ts, end_ts))

        assert len(ranges) == 1
        assert ranges[0]["start_ts"] == start_ts
        assert ranges[0]["end_ts"] == end_ts

    def test_one_day_generates_24_hours(self):
        """Test that a full day generates 24 hourly chunks."""
        start_ts = int(datetime(2024, 1, 1, 0, 0, 0).timestamp())
        end_ts = int(datetime(2024, 1, 2, 0, 0, 0).timestamp())

        ranges = list(generate_date_ranges(start_ts, end_ts))

        assert len(ranges) == 24
        assert ranges[0]["start_ts"] == start_ts
        assert ranges[0]["end_ts"] == int(datetime(2024, 1, 1, 1, 0, 0).timestamp())
        assert ranges[-1]["end_ts"] == end_ts

    def test_partial_hour_start(self):
        """Test starting in the middle of an hour."""
        start_ts = int(datetime(2024, 1, 1, 14, 30, 0).timestamp())
        end_ts = int(datetime(2024, 1, 1, 18, 0, 0).timestamp())

        ranges = list(generate_date_ranges(start_ts, end_ts))

        # 14:30->15:00, 15:00->16:00, 16:00->17:00, 17:00->18:00 = 4 chunks
        assert len(ranges) == 4
        assert ranges[0]["start_ts"] == start_ts
        assert ranges[0]["end_ts"] == int(datetime(2024, 1, 1, 15, 0, 0).timestamp())

    def test_partial_hour_end(self):
        """Test ending in the middle of an hour."""
        start_ts = int(datetime(2024, 1, 1, 10, 0, 0).timestamp())
        end_ts = int(datetime(2024, 1, 1, 12, 45, 0).timestamp())

        ranges = list(generate_date_ranges(start_ts, end_ts))

        # 10:00->11:00, 11:00->12:00, 12:00->12:45 = 3 chunks
        assert len(ranges) == 3
        assert ranges[-1]["end_ts"] == end_ts

    def test_multiple_days(self):
        """Test multiple days generates hourly chunks."""
        start_ts = int(datetime(2024, 1, 1, 0, 0, 0).timestamp())
        end_ts = int(datetime(2024, 1, 3, 0, 0, 0).timestamp())

        ranges = list(generate_date_ranges(start_ts, end_ts))

        # 2 days = 48 hours
        assert len(ranges) == 48

    def test_empty_range(self):
        """Test when start equals end."""
        ts = int(datetime(2024, 1, 1, 0, 0, 0).timestamp())

        ranges = list(generate_date_ranges(ts, ts))

        assert len(ranges) == 0

    def test_start_after_end(self):
        """Test when start is after end returns empty."""
        start_ts = int(datetime(2024, 1, 5, 0, 0, 0).timestamp())
        end_ts = int(datetime(2024, 1, 1, 0, 0, 0).timestamp())

        ranges = list(generate_date_ranges(start_ts, end_ts))

        assert len(ranges) == 0

    def test_week_range(self):
        """Test a full week generates 168 hourly ranges."""
        start_ts = int(datetime(2024, 1, 1, 0, 0, 0).timestamp())
        end_ts = int(datetime(2024, 1, 8, 0, 0, 0).timestamp())

        ranges = list(generate_date_ranges(start_ts, end_ts))

        # 7 days * 24 hours = 168 chunks
        assert len(ranges) == 168

    def test_ranges_are_contiguous(self):
        """Test that ranges cover the entire period without gaps."""
        start_ts = int(datetime(2024, 1, 1, 6, 30, 0).timestamp())
        end_ts = int(datetime(2024, 1, 1, 18, 45, 0).timestamp())

        ranges = list(generate_date_ranges(start_ts, end_ts))

        assert ranges[0]["start_ts"] == start_ts
        assert ranges[-1]["end_ts"] == end_ts
        for i in range(len(ranges) - 1):
            assert ranges[i]["end_ts"] == ranges[i + 1]["start_ts"]

    def test_returns_dict_with_correct_keys(self):
        """Test that each yielded item has the expected keys."""
        start_ts = int(datetime(2024, 1, 1, 0, 0, 0).timestamp())
        end_ts = int(datetime(2024, 1, 1, 1, 0, 0).timestamp())

        ranges = list(generate_date_ranges(start_ts, end_ts))

        assert len(ranges) == 1
        assert "start_ts" in ranges[0]
        assert "end_ts" in ranges[0]
        assert len(ranges[0]) == 2

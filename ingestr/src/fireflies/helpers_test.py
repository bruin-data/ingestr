from unittest.mock import MagicMock, patch

import pytest

from ingestr.src.fireflies.helpers import (
    FirefliesAPI,
    apply_item_errors,
    check_graphql_errors,
    extract_item_errors,
)


class TestCheckGraphqlErrors:
    def test_no_errors(self):
        """Should not raise when no errors in result."""
        result = {"data": {"users": []}}
        check_graphql_errors(result)

    def test_raises_on_errors(self):
        """Should raise ValueError when errors exist."""
        result = {
            "errors": [
                {"message": "First error"},
                {"message": "Second error"},
            ]
        }
        with pytest.raises(ValueError, match="First error"):
            check_graphql_errors(result)

    def test_handles_missing_message(self):
        """Should use 'Unknown error' when message is missing."""
        result = {"errors": [{}]}
        with pytest.raises(ValueError, match="Unknown error"):
            check_graphql_errors(result)


class TestExtractItemErrors:
    def test_no_errors(self):
        """Should return empty dict when no errors."""
        result = {"data": {"bites": [{"id": 1}]}}
        items = [{"id": 1}]

        errors = extract_item_errors(result, items, "bites")

        assert errors == {}

    def test_extracts_per_item_errors(self):
        """Should extract errors mapped to item indices."""
        result = {
            "data": {"bites": [{"id": 1}, {"id": 2}]},
            "errors": [
                {"path": ["bites", 0, "thumbnail"], "message": "Not found"},
                {"path": ["bites", 1, "preview"], "message": "Access denied"},
            ],
        }
        items = [{"id": 1}, {"id": 2}]

        errors = extract_item_errors(result, items, "bites")

        assert errors == {0: ["thumbnail"], 1: ["preview"]}

    def test_multiple_errors_same_item(self):
        """Should collect multiple errors for the same item."""
        result = {
            "data": {"bites": [{"id": 1}]},
            "errors": [
                {"path": ["bites", 0, "thumbnail"], "message": "Error 1"},
                {"path": ["bites", 0, "preview"], "message": "Error 2"},
            ],
        }
        items = [{"id": 1}]

        errors = extract_item_errors(result, items, "bites")

        assert errors == {0: ["thumbnail", "preview"]}

    def test_raises_when_errors_but_no_data(self):
        """Should raise ValueError when errors exist but no data."""
        result = {"errors": [{"message": "Fatal error"}]}
        items = []

        with pytest.raises(ValueError):
            extract_item_errors(result, items, "bites")


class TestApplyItemErrors:
    def test_applies_errors_to_items(self):
        """Should set error field on items with errors."""
        items = [{"id": 1}, {"id": 2}, {"id": 3}]
        errors_by_index = {0: ["field1", "field2"], 2: ["field3"]}

        apply_item_errors(items, errors_by_index)

        assert items[0]["error"] == "field1, field2"
        assert items[1]["error"] is None
        assert items[2]["error"] == "field3"

    def test_sets_none_for_items_without_errors(self):
        """Should set error to None for items without errors."""
        items = [{"id": 1}, {"id": 2}]
        errors_by_index = {}

        apply_item_errors(items, errors_by_index)

        assert items[0]["error"] is None
        assert items[1]["error"] is None


class TestFetchAnalyticsChunking:
    @patch.object(FirefliesAPI, "_fetch_analytics_chunk")
    def test_single_chunk_when_under_30_days(self, mock_fetch_chunk):
        """Should make single request when date range is <= 30 days."""
        mock_fetch_chunk.return_value = iter([[{"team": {}}]])
        api = FirefliesAPI(api_key="test-api-key")

        from_date = "2024-01-01T00:00:00+00:00"
        to_date = "2024-01-20T00:00:00+00:00"  # 19 days

        list(api.fetch_analytics(from_date, to_date))

        assert mock_fetch_chunk.call_count == 1

    @patch.object(FirefliesAPI, "_fetch_analytics_chunk")
    def test_multiple_chunks_when_over_30_days(self, mock_fetch_chunk):
        """Should split into multiple chunks when date range > 30 days."""
        mock_fetch_chunk.return_value = iter([[{"team": {}}]])
        api = FirefliesAPI(api_key="test-api-key")

        from_date = "2024-01-01T00:00:00+00:00"
        to_date = "2024-03-01T00:00:00+00:00"  # 60 days

        list(api.fetch_analytics(from_date, to_date))

        # 60 days should result in 2 chunks
        assert mock_fetch_chunk.call_count == 2

    @patch.object(FirefliesAPI, "_fetch_analytics_chunk")
    def test_exactly_30_days_single_chunk(self, mock_fetch_chunk):
        """Should make single request when date range is exactly 30 days."""
        mock_fetch_chunk.return_value = iter([[{"team": {}}]])
        api = FirefliesAPI(api_key="test-api-key")

        from_date = "2024-01-01T00:00:00+00:00"
        to_date = "2024-01-31T00:00:00+00:00"  # 30 days

        list(api.fetch_analytics(from_date, to_date))

        assert mock_fetch_chunk.call_count == 1

    @patch.object(FirefliesAPI, "_fetch_analytics_chunk")
    def test_chunk_boundaries_are_correct(self, mock_fetch_chunk):
        """Should have correct start/end dates for each chunk."""
        mock_fetch_chunk.return_value = iter([[{"team": {}}]])
        api = FirefliesAPI(api_key="test-api-key")

        from_date = "2024-01-01T00:00:00+00:00"
        to_date = "2024-02-15T00:00:00+00:00"  # 45 days

        list(api.fetch_analytics(from_date, to_date))

        calls = mock_fetch_chunk.call_args_list
        assert len(calls) == 2

        # First chunk: Jan 1 - Jan 31
        first_start = calls[0][0][0]
        first_end = calls[0][0][1]
        assert "2024-01-01" in first_start
        assert "2024-01-31" in first_end

        # Second chunk: Feb 1 - Feb 15
        second_start = calls[1][0][0]
        second_end = calls[1][0][1]
        assert "2024-02-01" in second_start
        assert "2024-02-15" in second_end

    @patch.object(FirefliesAPI, "_fetch_analytics_chunk")
    def test_91_days_creates_3_chunks(self, mock_fetch_chunk):
        """Should create 3 chunks for 91 days (Jan1-Jan31, Feb1-Mar2, Mar3-Apr1)."""
        mock_fetch_chunk.return_value = iter([[{"team": {}}]])
        api = FirefliesAPI(api_key="test-api-key")

        from_date = "2024-01-01T00:00:00+00:00"
        to_date = "2024-04-01T00:00:00+00:00"  # 91 days

        list(api.fetch_analytics(from_date, to_date))

        # 91 days with 30-day chunks: Jan1-Jan31, Feb1-Mar2, Mar3-Apr1 = 3 chunks
        assert mock_fetch_chunk.call_count == 3


class TestFetchAnalyticsChunkResponse:
    @patch("ingestr.src.fireflies.helpers.create_client")
    def test_adds_start_end_time_to_result(self, mock_create_client):
        """Should add start_time and end_time to analytics result."""
        mock_client = MagicMock()
        mock_create_client.return_value = mock_client

        mock_response = MagicMock()
        mock_response.json.return_value = {
            "data": {
                "analytics": {
                    "team": {"meeting": {"count": 10}},
                    "users": [],
                }
            }
        }
        mock_client.post.return_value = mock_response

        api = FirefliesAPI(api_key="test-key")
        result = list(api._fetch_analytics_chunk("2024-01-01", "2024-01-31"))

        assert len(result) == 1
        assert result[0][0]["start_time"] == "2024-01-01"
        assert result[0][0]["end_time"] == "2024-01-31"

    @patch("ingestr.src.fireflies.helpers.create_client")
    def test_yields_nothing_when_no_analytics(self, mock_create_client):
        """Should yield nothing when analytics is empty."""
        mock_client = MagicMock()
        mock_create_client.return_value = mock_client

        mock_response = MagicMock()
        mock_response.json.return_value = {"data": {"analytics": {}}}
        mock_client.post.return_value = mock_response

        api = FirefliesAPI(api_key="test-key")
        result = list(api._fetch_analytics_chunk("2024-01-01", "2024-01-31"))

        assert len(result) == 0


class TestFetchAnalyticsDaily:
    @patch.object(FirefliesAPI, "_fetch_analytics_chunk")
    def test_creates_daily_chunks(self, mock_fetch_chunk):
        """Should create one chunk per day."""
        mock_fetch_chunk.return_value = iter([[{"team": {}}]])
        api = FirefliesAPI(api_key="test-api-key")

        from_date = "2024-04-01T00:00:00+00:00"
        to_date = "2024-04-05T00:00:00+00:00"  # 4 days

        list(api.fetch_analytics_daily(from_date, to_date))

        assert mock_fetch_chunk.call_count == 4

    @patch.object(FirefliesAPI, "_fetch_analytics_chunk")
    def test_daily_chunk_boundaries(self, mock_fetch_chunk):
        """Should have correct start/end for each daily chunk."""
        mock_fetch_chunk.return_value = iter([[{"team": {}}]])
        api = FirefliesAPI(api_key="test-api-key")

        from_date = "2024-04-01T00:00:00+00:00"
        to_date = "2024-04-03T00:00:00+00:00"

        list(api.fetch_analytics_daily(from_date, to_date))

        calls = mock_fetch_chunk.call_args_list
        assert len(calls) == 2

        # Day 1: Apr 1 -> Apr 2
        assert "2024-04-01" in calls[0][0][0]
        assert "2024-04-02" in calls[0][0][1]

        # Day 2: Apr 2 -> Apr 3
        assert "2024-04-02" in calls[1][0][0]
        assert "2024-04-03" in calls[1][0][1]

    @patch.object(FirefliesAPI, "_fetch_analytics_chunk")
    def test_single_day_range(self, mock_fetch_chunk):
        """Should handle single day range correctly."""
        mock_fetch_chunk.return_value = iter([[{"team": {}}]])
        api = FirefliesAPI(api_key="test-api-key")

        from_date = "2024-04-01T00:00:00+00:00"
        to_date = "2024-04-02T00:00:00+00:00"

        list(api.fetch_analytics_daily(from_date, to_date))

        assert mock_fetch_chunk.call_count == 1

    @patch.object(FirefliesAPI, "_fetch_analytics_chunk")
    def test_daily_respects_start_time_then_aligns(self, mock_fetch_chunk):
        """Should start from exact time, then align to midnight boundaries."""
        mock_fetch_chunk.return_value = iter([[{"team": {}}]])
        api = FirefliesAPI(api_key="test-api-key")

        # Start in the middle of a day
        from_date = "2024-04-01T14:30:00+00:00"
        to_date = "2024-04-03T10:00:00+00:00"

        list(api.fetch_analytics_daily(from_date, to_date))

        calls = mock_fetch_chunk.call_args_list
        assert len(calls) == 3

        # First chunk: Apr 1 14:30 -> Apr 2 00:00 (to midnight)
        assert "2024-04-01T14:30:00" in calls[0][0][0]
        assert "2024-04-02T00:00:00" in calls[0][0][1]

        # Second chunk: Apr 2 00:00 -> Apr 3 00:00 (day aligned)
        assert "2024-04-02T00:00:00" in calls[1][0][0]
        assert "2024-04-03T00:00:00" in calls[1][0][1]

        # Third chunk: Apr 3 00:00 -> Apr 3 10:00 (capped by end)
        assert "2024-04-03T00:00:00" in calls[2][0][0]
        assert "2024-04-03T10:00:00" in calls[2][0][1]


class TestFetchAnalyticsHourly:
    @patch.object(FirefliesAPI, "_fetch_analytics_chunk")
    def test_creates_hourly_chunks(self, mock_fetch_chunk):
        """Should create one chunk per hour."""
        mock_fetch_chunk.return_value = iter([[{"team": {}}]])
        api = FirefliesAPI(api_key="test-api-key")

        from_date = "2024-04-01T00:00:00+00:00"
        to_date = "2024-04-01T05:00:00+00:00"  # 5 hours

        list(api.fetch_analytics_hourly(from_date, to_date))

        assert mock_fetch_chunk.call_count == 5

    @patch.object(FirefliesAPI, "_fetch_analytics_chunk")
    def test_hourly_chunk_boundaries(self, mock_fetch_chunk):
        """Should have correct start/end for each hourly chunk."""
        mock_fetch_chunk.return_value = iter([[{"team": {}}]])
        api = FirefliesAPI(api_key="test-api-key")

        from_date = "2024-04-01T10:00:00+00:00"
        to_date = "2024-04-01T12:00:00+00:00"

        list(api.fetch_analytics_hourly(from_date, to_date))

        calls = mock_fetch_chunk.call_args_list
        assert len(calls) == 2

        # Hour 1: 10:00 -> 11:00
        assert "T10:00:00" in calls[0][0][0]
        assert "T11:00:00" in calls[0][0][1]

        # Hour 2: 11:00 -> 12:00
        assert "T11:00:00" in calls[1][0][0]
        assert "T12:00:00" in calls[1][0][1]

    @patch.object(FirefliesAPI, "_fetch_analytics_chunk")
    def test_hourly_across_days(self, mock_fetch_chunk):
        """Should handle hourly chunks that span multiple days."""
        mock_fetch_chunk.return_value = iter([[{"team": {}}]])
        api = FirefliesAPI(api_key="test-api-key")

        from_date = "2024-04-01T22:00:00+00:00"
        to_date = "2024-04-02T02:00:00+00:00"  # 4 hours across midnight

        list(api.fetch_analytics_hourly(from_date, to_date))

        assert mock_fetch_chunk.call_count == 4

    @patch.object(FirefliesAPI, "_fetch_analytics_chunk")
    def test_hourly_respects_start_time_then_aligns(self, mock_fetch_chunk):
        """Should start from exact time, then align to hour boundaries."""
        mock_fetch_chunk.return_value = iter([[{"team": {}}]])
        api = FirefliesAPI(api_key="test-api-key")

        # Start in the middle of an hour
        from_date = "2024-04-01T10:30:00+00:00"
        to_date = "2024-04-01T12:45:00+00:00"

        list(api.fetch_analytics_hourly(from_date, to_date))

        calls = mock_fetch_chunk.call_args_list
        assert len(calls) == 3

        # First chunk: 10:30 -> 11:00 (to next hour boundary)
        assert "T10:30:00" in calls[0][0][0]
        assert "T11:00:00" in calls[0][0][1]

        # Second chunk: 11:00 -> 12:00 (hour aligned)
        assert "T11:00:00" in calls[1][0][0]
        assert "T12:00:00" in calls[1][0][1]

        # Third chunk: 12:00 -> 12:45 (capped by end)
        assert "T12:00:00" in calls[2][0][0]
        assert "T12:45:00" in calls[2][0][1]


class TestFetchAnalyticsMonthly:
    @patch.object(FirefliesAPI, "_fetch_analytics_chunk")
    def test_creates_monthly_chunks(self, mock_fetch_chunk):
        """Should create one chunk per month."""
        mock_fetch_chunk.return_value = iter([[{"team": {}}]])
        api = FirefliesAPI(api_key="test-api-key")

        from_date = "2024-01-01T00:00:00+00:00"
        to_date = "2024-04-01T00:00:00+00:00"  # 3 months

        list(api.fetch_analytics_monthly(from_date, to_date))

        assert mock_fetch_chunk.call_count == 3

    @patch.object(FirefliesAPI, "_fetch_analytics_chunk")
    def test_monthly_first_to_last_day_alignment(self, mock_fetch_chunk):
        """Should align chunks from first day to last day of each month."""
        mock_fetch_chunk.return_value = iter([[{"team": {}}]])
        api = FirefliesAPI(api_key="test-api-key")

        from_date = "2024-01-01T00:00:00+00:00"
        to_date = "2024-03-31T00:00:00+00:00"

        list(api.fetch_analytics_monthly(from_date, to_date))

        calls = mock_fetch_chunk.call_args_list
        assert len(calls) == 3

        # January: Jan 1 -> Jan 31
        assert "2024-01-01" in calls[0][0][0]
        assert "2024-01-31" in calls[0][0][1]

        # February: Feb 1 -> Feb 29 (2024 is leap year)
        assert "2024-02-01" in calls[1][0][0]
        assert "2024-02-29" in calls[1][0][1]

        # March: Mar 1 -> Mar 31 (capped by end date)
        assert "2024-03-01" in calls[2][0][0]
        assert "2024-03-31" in calls[2][0][1]

    @patch.object(FirefliesAPI, "_fetch_analytics_chunk")
    def test_monthly_respects_end_date_mid_month(self, mock_fetch_chunk):
        """Should cap chunk end to provided end date if it's mid-month."""
        mock_fetch_chunk.return_value = iter([[{"team": {}}]])
        api = FirefliesAPI(api_key="test-api-key")

        from_date = "2024-01-01T00:00:00+00:00"
        to_date = "2024-01-15T00:00:00+00:00"  # Mid-January

        list(api.fetch_analytics_monthly(from_date, to_date))

        calls = mock_fetch_chunk.call_args_list
        assert len(calls) == 1

        # Should be Jan 1 -> Jan 15 (not Jan 31)
        assert "2024-01-01" in calls[0][0][0]
        assert "2024-01-15" in calls[0][0][1]

    @patch.object(FirefliesAPI, "_fetch_analytics_chunk")
    def test_monthly_respects_start_date(self, mock_fetch_chunk):
        """Should respect user's start date, not align to start of month."""
        mock_fetch_chunk.return_value = iter([[{"team": {}}]])
        api = FirefliesAPI(api_key="test-api-key")

        # Start mid-month
        from_date = "2024-01-15T00:00:00+00:00"
        to_date = "2024-02-28T00:00:00+00:00"

        list(api.fetch_analytics_monthly(from_date, to_date))

        calls = mock_fetch_chunk.call_args_list
        assert len(calls) == 2

        # First chunk: Jan 15 -> Jan 31 (respects start date)
        assert "2024-01-15" in calls[0][0][0]
        assert "2024-01-31" in calls[0][0][1]

        # Second chunk: Feb 1 -> Feb 28
        assert "2024-02-01" in calls[1][0][0]
        assert "2024-02-28" in calls[1][0][1]

    @patch.object(FirefliesAPI, "_fetch_analytics_chunk")
    def test_monthly_handles_full_year(self, mock_fetch_chunk):
        """Should handle a full year correctly (12 months)."""
        mock_fetch_chunk.return_value = iter([[{"team": {}}]])
        api = FirefliesAPI(api_key="test-api-key")

        from_date = "2024-01-01T00:00:00+00:00"
        to_date = "2024-12-31T00:00:00+00:00"

        list(api.fetch_analytics_monthly(from_date, to_date))

        # 12 months in a year
        assert mock_fetch_chunk.call_count == 12

    @patch.object(FirefliesAPI, "_fetch_analytics_chunk")
    def test_monthly_february_leap_year(self, mock_fetch_chunk):
        """Should handle February correctly in leap year (29 days)."""
        mock_fetch_chunk.return_value = iter([[{"team": {}}]])
        api = FirefliesAPI(api_key="test-api-key")

        from_date = "2024-02-01T00:00:00+00:00"
        to_date = "2024-03-01T00:00:00+00:00"

        list(api.fetch_analytics_monthly(from_date, to_date))

        calls = mock_fetch_chunk.call_args_list
        assert len(calls) == 1

        # Feb 1 -> Feb 29 (2024 is leap year)
        assert "2024-02-01" in calls[0][0][0]
        assert "2024-02-29" in calls[0][0][1]

    @patch.object(FirefliesAPI, "_fetch_analytics_chunk")
    def test_monthly_february_non_leap_year(self, mock_fetch_chunk):
        """Should handle February correctly in non-leap year (28 days)."""
        mock_fetch_chunk.return_value = iter([[{"team": {}}]])
        api = FirefliesAPI(api_key="test-api-key")

        from_date = "2023-02-01T00:00:00+00:00"
        to_date = "2023-03-01T00:00:00+00:00"

        list(api.fetch_analytics_monthly(from_date, to_date))

        calls = mock_fetch_chunk.call_args_list
        assert len(calls) == 1

        # Feb 1 -> Feb 28 (2023 is not leap year)
        assert "2023-02-01" in calls[0][0][0]
        assert "2023-02-28" in calls[0][0][1]

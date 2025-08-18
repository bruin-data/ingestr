"""
Unit tests for RevenueCat helper functions.
"""
import pytest
import pendulum
import asyncio
import aiohttp
from typing import Dict, Any
from unittest.mock import Mock, patch, AsyncMock

from .helpers import convert_timestamps_to_iso, _make_request, _paginate, _make_request_async, _paginate_async


class TestConvertTimestampsToIso:
    """Tests for convert_timestamps_to_iso function."""
    
    def test_convert_single_timestamp_field(self):
        """Test converting a single timestamp field from milliseconds to ISO format."""
        # January 1, 2024 00:00:00 UTC in milliseconds
        timestamp_ms = 1704067200000
        record = {"created_at": timestamp_ms, "name": "test_record"}
        timestamp_fields = ["created_at"]
        
        result = convert_timestamps_to_iso(record, timestamp_fields)
        
        assert result["created_at"] == "2024-01-01T00:00:00Z"
        assert result["name"] == "test_record"
        
    def test_convert_multiple_timestamp_fields(self):
        """Test converting multiple timestamp fields."""
        timestamp_ms_1 = 1704067200000  # January 1, 2024 00:00:00 UTC
        timestamp_ms_2 = 1704153600000  # January 2, 2024 00:00:00 UTC
        
        record = {
            "created_at": timestamp_ms_1,
            "updated_at": timestamp_ms_2,
            "name": "test_record"
        }
        timestamp_fields = ["created_at", "updated_at"]
        
        result = convert_timestamps_to_iso(record, timestamp_fields)
        
        assert result["created_at"] == "2024-01-01T00:00:00Z"
        assert result["updated_at"] == "2024-01-02T00:00:00Z"
        assert result["name"] == "test_record"


class TestMakeRequest:
    """Tests for _make_request function."""
    
    @patch('requests.get')
    def test_successful_request(self, mock_get):
        """Test successful API request."""
        mock_response = Mock()
        mock_response.status_code = 200
        mock_response.json.return_value = {"data": "test"}
        mock_get.return_value = mock_response
        
        result = _make_request("test_api_key", "/test", {"param": "value"})
        
        assert result == {"data": "test"}
        mock_get.assert_called_once()
        
    @patch('requests.get')
    def test_rate_limit_retry(self, mock_get):
        """Test rate limit handling with retry."""
        # First call returns 429, second call succeeds
        mock_response_429 = Mock()
        mock_response_429.status_code = 429
        mock_response_429.headers = {'Retry-After': '1'}
        
        mock_response_200 = Mock()
        mock_response_200.status_code = 200
        mock_response_200.json.return_value = {"data": "success"}
        
        mock_get.side_effect = [mock_response_429, mock_response_200]
        
        with patch('time.sleep') as mock_sleep:
            result = _make_request("test_api_key", "/test", max_retries=1)
            
        assert result == {"data": "success"}
        mock_sleep.assert_called_once_with(1)


class TestPaginate:
    """Tests for _paginate function."""
    
    @patch('revenuecat.helpers._make_request')
    def test_single_page_pagination(self, mock_make_request):
        """Test pagination with single page."""
        mock_make_request.return_value = {
            "items": [{"id": 1}, {"id": 2}]
        }
        
        results = list(_paginate("test_api_key", "/test"))
        
        assert len(results) == 2
        assert results[0] == {"id": 1}
        assert results[1] == {"id": 2}
        mock_make_request.assert_called_once()
        
    @patch('revenuecat.helpers._make_request')
    def test_multi_page_pagination(self, mock_make_request):
        """Test pagination with multiple pages."""
        # First page with next_page URL
        mock_make_request.side_effect = [
            {
                "items": [{"id": 1}],
                "next_page": "https://api.example.com/test?starting_after=1&limit=1000"
            },
            {
                "items": [{"id": 2}]
            }
        ]
        
        results = list(_paginate("test_api_key", "/test"))
        
        assert len(results) == 2
        assert results[0] == {"id": 1}
        assert results[1] == {"id": 2}
        assert mock_make_request.call_count == 2


class TestAsyncFunctions:
    """Tests for async helper functions."""
    
    async def test_async_function_signature(self):
        """Test that async functions exist and have correct signatures."""
        # Test that functions are callable and async
        assert asyncio.iscoroutinefunction(_make_request_async)
        assert asyncio.iscoroutinefunction(_paginate_async)
        
    async def test_async_sleep_patch(self):
        """Test async sleep can be patched (integration test)."""
        with patch('asyncio.sleep', new_callable=AsyncMock) as mock_sleep:
            await asyncio.sleep(0.1)
            mock_sleep.assert_called_once_with(0.1)


class TestPaginateAsync:
    """Tests for _paginate_async function."""
    
    async def test_async_single_page(self):
        """Test async pagination with single page."""
        mock_session = AsyncMock()
        
        with patch('revenuecat.helpers._make_request_async') as mock_make_request:
            mock_make_request.return_value = {
                "items": [{"id": 1}, {"id": 2}]
            }
            
            results = await _paginate_async(mock_session, "test_key", "/test")
            
        assert len(results) == 2
        assert results[0] == {"id": 1}
        assert results[1] == {"id": 2}
        mock_make_request.assert_called_once()

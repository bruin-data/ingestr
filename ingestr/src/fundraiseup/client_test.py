"""Tests for FundraiseupClient."""

import sys
import pytest
from unittest.mock import Mock, patch


class TestFundraiseupClient:
    """Test suite for FundraiseupClient."""

    @patch("ingestr.src.http_client.create_client")
    def test_client_initialization(self, mock_create_client):
        """Test that client initializes with correct API key and base URL."""
        mock_client = Mock()
        mock_create_client.return_value = mock_client
        
        # Clear module cache to ensure clean import with mocks
        if 'ingestr.src.fundraiseup.client' in sys.modules:
            del sys.modules['ingestr.src.fundraiseup.client']
        from ingestr.src.fundraiseup.client import FundraiseupClient
        client = FundraiseupClient(api_key="test_key_123")
        
        assert client.api_key == "test_key_123"
        assert client.base_url == "https://api.fundraiseup.com/v1"
        assert client.client == mock_client
        mock_create_client.assert_called_once_with(retry_status_codes=[429, 500, 502, 503, 504])

    @patch("ingestr.src.http_client.create_client")
    def test_get_paginated_data_single_page_list(self, mock_create_client):
        """Test pagination with a single page returning a list."""
        # Setup mock client
        mock_client = Mock()
        mock_create_client.return_value = mock_client
        
        # Mock response for a single page (list format)
        mock_response = Mock()
        mock_response.json = Mock(return_value=[
            {"id": "1", "name": "Item 1"},
            {"id": "2", "name": "Item 2"},
        ])
        mock_response.raise_for_status = Mock()
        mock_client.get.return_value = mock_response

        # Create client and get data
        # Clear module cache to ensure clean import with mocks
        if 'ingestr.src.fundraiseup.client' in sys.modules:
            del sys.modules['ingestr.src.fundraiseup.client']
        from ingestr.src.fundraiseup.client import FundraiseupClient
        client = FundraiseupClient(api_key="test_api_key")
        batches = list(client.get_paginated_data("test_endpoint"))

        # Assertions
        assert len(batches) == 1
        assert len(batches[0]) == 2
        assert batches[0][0]["id"] == "1"
        assert batches[0][1]["id"] == "2"
        mock_client.get.assert_called_once_with(
            url="https://api.fundraiseup.com/v1/test_endpoint",
            headers={
                "Authorization": "Bearer test_api_key",
                "Content-Type": "application/json"
            },
            params={"limit": 100}
        )

    @patch("ingestr.src.http_client.create_client")
    def test_get_paginated_data_single_page_object(self, mock_create_client):
        """Test pagination with a single page returning an object with data key."""
        # Setup mock client
        mock_client = Mock()
        mock_create_client.return_value = mock_client
        
        # Mock response for a single page (object format)
        mock_response = Mock()
        mock_response.json = Mock(return_value={
            "data": [
                {"id": "1", "name": "Item 1"},
                {"id": "2", "name": "Item 2"},
            ]
        })
        mock_response.raise_for_status = Mock()
        mock_client.get.return_value = mock_response

        # Create client and get data
        # Clear module cache to ensure clean import with mocks
        if 'ingestr.src.fundraiseup.client' in sys.modules:
            del sys.modules['ingestr.src.fundraiseup.client']
        from ingestr.src.fundraiseup.client import FundraiseupClient
        client = FundraiseupClient(api_key="test_api_key")
        batches = list(client.get_paginated_data("test_endpoint"))

        # Assertions
        assert len(batches) == 1
        assert len(batches[0]) == 2
        assert batches[0][0]["id"] == "1"
        assert batches[0][1]["id"] == "2"

    @patch("ingestr.src.http_client.create_client")
    def test_get_paginated_data_multiple_pages(self, mock_create_client):
        """Test pagination with multiple pages using cursor."""
        # Setup mock client
        mock_client = Mock()
        mock_create_client.return_value = mock_client
        
        # Mock responses for multiple pages
        response1 = Mock()
        response1.json = Mock(return_value=[
            {"id": f"{i}", "name": f"Item {i}"} for i in range(1, 101)
        ])
        response1.raise_for_status = Mock()
        
        response2 = Mock()
        response2.json = Mock(return_value=[
            {"id": f"{i}", "name": f"Item {i}"} for i in range(101, 151)
        ])
        response2.raise_for_status = Mock()
        
        responses = [response1, response2]
        
        mock_client.get.side_effect = responses

        # Create client and get data
        # Clear module cache to ensure clean import with mocks
        if 'ingestr.src.fundraiseup.client' in sys.modules:
            del sys.modules['ingestr.src.fundraiseup.client']
        from ingestr.src.fundraiseup.client import FundraiseupClient
        client = FundraiseupClient(api_key="test_api_key")
        batches = list(client.get_paginated_data("test_endpoint"))

        # Assertions
        assert len(batches) == 2
        assert len(batches[0]) == 100
        assert len(batches[1]) == 50
        assert batches[0][0]["id"] == "1"
        assert batches[0][-1]["id"] == "100"
        assert batches[1][0]["id"] == "101"
        assert batches[1][-1]["id"] == "150"
        
        # Check API calls (should stop after second call since it returned < page_size)
        assert mock_client.get.call_count == 2
        calls = mock_client.get.call_args_list
        assert calls[0].kwargs["url"] == "https://api.fundraiseup.com/v1/test_endpoint"
        # The params dict is mutated in place, so we can only check what keys are present
        assert "limit" in calls[0].kwargs["params"]
        assert calls[0].kwargs["params"]["limit"] == 100
        # Second call should have starting_after
        assert "starting_after" in calls[1].kwargs["params"]
        assert calls[1].kwargs["params"]["starting_after"] == "100"

    @patch("ingestr.src.http_client.create_client")
    def test_get_paginated_data_custom_page_size(self, mock_create_client):
        """Test pagination with custom page size."""
        # Setup mock client
        mock_client = Mock()
        mock_create_client.return_value = mock_client
        
        # First response with 50 items (less than default page size of 100)
        mock_response = Mock()
        mock_response.json = Mock(return_value=[
            {"id": f"{i}", "name": f"Item {i}"} for i in range(1, 50)  # Only 49 items (less than page_size)
        ])
        mock_response.raise_for_status = Mock()
        mock_client.get.return_value = mock_response

        # Create client and get data with custom page size
        # Clear module cache to ensure clean import with mocks
        if 'ingestr.src.fundraiseup.client' in sys.modules:
            del sys.modules['ingestr.src.fundraiseup.client']
        from ingestr.src.fundraiseup.client import FundraiseupClient
        client = FundraiseupClient(api_key="test_api_key")
        batches = list(client.get_paginated_data("test_endpoint", page_size=50))

        # Assertions
        assert len(batches) == 1
        assert len(batches[0]) == 49  # Only 49 items returned
        mock_client.get.assert_called_once_with(
            url="https://api.fundraiseup.com/v1/test_endpoint",
            headers={
                "Authorization": "Bearer test_api_key",
                "Content-Type": "application/json"
            },
            params={"limit": 50}
        )

    @patch("ingestr.src.http_client.create_client")
    def test_get_paginated_data_with_additional_params(self, mock_create_client):
        """Test pagination with additional query parameters."""
        # Setup mock client
        mock_client = Mock()
        mock_create_client.return_value = mock_client
        
        mock_response = Mock()
        mock_response.json = Mock(return_value=[
            {"id": "1", "status": "active"},
        ])
        mock_response.raise_for_status = Mock()
        mock_client.get.return_value = mock_response

        # Create client and get data with additional params
        # Clear module cache to ensure clean import with mocks
        if 'ingestr.src.fundraiseup.client' in sys.modules:
            del sys.modules['ingestr.src.fundraiseup.client']
        from ingestr.src.fundraiseup.client import FundraiseupClient
        client = FundraiseupClient(api_key="test_api_key")
        params = {"status": "active", "type": "donation"}
        batches = list(client.get_paginated_data("test_endpoint", params=params))

        # Assertions
        assert len(batches) == 1
        mock_client.get.assert_called_once_with(
            url="https://api.fundraiseup.com/v1/test_endpoint",
            headers={
                "Authorization": "Bearer test_api_key",
                "Content-Type": "application/json"
            },
            params={"limit": 100, "status": "active", "type": "donation"}
        )

    @patch("ingestr.src.http_client.create_client")
    def test_get_paginated_data_empty_response(self, mock_create_client):
        """Test pagination with empty response."""
        # Setup mock client
        mock_client = Mock()
        mock_create_client.return_value = mock_client
        
        mock_response = Mock()
        mock_response.json = Mock(return_value=[])
        mock_response.raise_for_status = Mock()
        mock_client.get.return_value = mock_response

        # Create client and get data
        # Clear module cache to ensure clean import with mocks
        if 'ingestr.src.fundraiseup.client' in sys.modules:
            del sys.modules['ingestr.src.fundraiseup.client']
        from ingestr.src.fundraiseup.client import FundraiseupClient
        client = FundraiseupClient(api_key="test_api_key")
        batches = list(client.get_paginated_data("test_endpoint"))

        # Assertions
        assert len(batches) == 0
        mock_client.get.assert_called_once()

    @patch("ingestr.src.http_client.create_client")
    def test_get_paginated_data_empty_data_key(self, mock_create_client):
        """Test pagination with empty data key in object response."""
        # Setup mock client
        mock_client = Mock()
        mock_create_client.return_value = mock_client
        
        mock_response = Mock()
        mock_response.json = Mock(return_value={"data": []})
        mock_response.raise_for_status = Mock()
        mock_client.get.return_value = mock_response

        # Create client and get data
        # Clear module cache to ensure clean import with mocks
        if 'ingestr.src.fundraiseup.client' in sys.modules:
            del sys.modules['ingestr.src.fundraiseup.client']
        from ingestr.src.fundraiseup.client import FundraiseupClient
        client = FundraiseupClient(api_key="test_api_key")
        batches = list(client.get_paginated_data("test_endpoint"))

        # Assertions
        assert len(batches) == 0

    @patch("ingestr.src.http_client.create_client")
    def test_get_paginated_data_api_error(self, mock_create_client):
        """Test that API errors are properly raised."""
        # Setup mock client
        mock_client = Mock()
        mock_create_client.return_value = mock_client
        
        mock_response = Mock()
        mock_response.raise_for_status.side_effect = Exception("API Error")
        mock_client.get.return_value = mock_response

        # Create client - should raise the exception
        # Clear module cache to ensure clean import with mocks
        if 'ingestr.src.fundraiseup.client' in sys.modules:
            del sys.modules['ingestr.src.fundraiseup.client']
        from ingestr.src.fundraiseup.client import FundraiseupClient
        client = FundraiseupClient(api_key="test_api_key")
        with pytest.raises(Exception, match="API Error"):
            list(client.get_paginated_data("test_endpoint"))

    @patch("ingestr.src.http_client.create_client")
    def test_get_paginated_data_less_than_page_size(self, mock_create_client):
        """Test that pagination stops when receiving less items than page size."""
        # Setup mock client
        mock_client = Mock()
        mock_create_client.return_value = mock_client
        
        # First page with 100 items, second page with 30 items (less than page size)
        response1 = Mock()
        response1.json = Mock(return_value=[
            {"id": f"{i}", "name": f"Item {i}"} for i in range(1, 101)
        ])
        response1.raise_for_status = Mock()
        
        response2 = Mock()
        response2.json = Mock(return_value=[
            {"id": f"{i}", "name": f"Item {i}"} for i in range(101, 131)
        ])
        response2.raise_for_status = Mock()
        
        responses = [response1, response2]
        
        mock_client.get.side_effect = responses

        # Create client and get data
        # Clear module cache to ensure clean import with mocks
        if 'ingestr.src.fundraiseup.client' in sys.modules:
            del sys.modules['ingestr.src.fundraiseup.client']
        from ingestr.src.fundraiseup.client import FundraiseupClient
        client = FundraiseupClient(api_key="test_api_key")
        batches = list(client.get_paginated_data("test_endpoint"))

        # Assertions
        assert len(batches) == 2
        assert len(batches[0]) == 100
        assert len(batches[1]) == 30
        # Should only make 2 calls (not try for a third page)
        assert mock_client.get.call_count == 2

    @patch("ingestr.src.http_client.create_client")
    def test_all_endpoints(self, mock_create_client):
        """Test that client works with all expected endpoints."""
        # Setup mock client
        mock_client = Mock()
        mock_create_client.return_value = mock_client
        
        # Clear module cache to ensure clean import with mocks
        if 'ingestr.src.fundraiseup.client' in sys.modules:
            del sys.modules['ingestr.src.fundraiseup.client']
        from ingestr.src.fundraiseup.client import FundraiseupClient
        
        endpoints = [
            "donations",
            "events", 
            "fundraisers",
            "recurring_plans",
            "supporters"
        ]
        
        for endpoint in endpoints:
            mock_response = Mock()
            mock_response.json = Mock(return_value=[{"id": "test", "type": endpoint}])
            mock_response.raise_for_status = Mock()
            mock_client.get.return_value = mock_response
            
            client = FundraiseupClient(api_key="test_api_key")
            batches = list(client.get_paginated_data(endpoint))
            assert len(batches) == 1
            assert batches[0][0]["type"] == endpoint
            
            expected_url = f"https://api.fundraiseup.com/v1/{endpoint}"
            mock_client.get.assert_called_with(
                url=expected_url,
                headers={
                    "Authorization": "Bearer test_api_key",
                    "Content-Type": "application/json"
                },
                params={"limit": 100}
            )

    @patch("ingestr.src.http_client.create_client")
    def test_pagination_with_has_more_flag(self, mock_create_client):
        """Test pagination when API returns has_more flag in object response."""
        # Setup mock client
        mock_client = Mock()
        mock_create_client.return_value = mock_client
        
        # Mock responses with has_more flag
        response1 = Mock()
        response1.json = Mock(return_value={
            "data": [{"id": f"{i}", "name": f"Item {i}"} for i in range(1, 101)],
            "has_more": True
        })
        response1.raise_for_status = Mock()
        
        response2 = Mock()
        response2.json = Mock(return_value={
            "data": [{"id": f"{i}", "name": f"Item {i}"} for i in range(101, 151)],
            "has_more": False
        })
        response2.raise_for_status = Mock()
        
        responses = [response1, response2]
        
        mock_client.get.side_effect = responses

        # Create client and get data
        # Clear module cache to ensure clean import with mocks
        if 'ingestr.src.fundraiseup.client' in sys.modules:
            del sys.modules['ingestr.src.fundraiseup.client']
        from ingestr.src.fundraiseup.client import FundraiseupClient
        client = FundraiseupClient(api_key="test_api_key")
        batches = list(client.get_paginated_data("test_endpoint"))

        # Assertions
        assert len(batches) == 2
        assert len(batches[0]) == 100
        assert len(batches[1]) == 50
        assert mock_client.get.call_count == 2

    @patch("ingestr.src.http_client.create_client")
    def test_pagination_stops_on_missing_id(self, mock_create_client):
        """Test that pagination stops when items don't have IDs."""
        # Setup mock client
        mock_client = Mock()
        mock_create_client.return_value = mock_client
        
        # Mock response without IDs in items
        mock_response = Mock()
        mock_response.json = Mock(return_value=[
            {"name": "Item 1"},  # No ID field
            {"name": "Item 2"},
        ])
        mock_response.raise_for_status = Mock()
        mock_client.get.return_value = mock_response

        # Create client and get data
        # Clear module cache to ensure clean import with mocks
        if 'ingestr.src.fundraiseup.client' in sys.modules:
            del sys.modules['ingestr.src.fundraiseup.client']
        from ingestr.src.fundraiseup.client import FundraiseupClient
        client = FundraiseupClient(api_key="test_api_key")
        batches = list(client.get_paginated_data("test_endpoint"))

        # Should yield the batch but not try to paginate further
        assert len(batches) == 1
        assert len(batches[0]) == 2
        assert mock_client.get.call_count == 1
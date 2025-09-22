"""
Unit tests for Intercom helper functions and API client.
"""
import unittest
from unittest.mock import MagicMock, Mock, patch
from dataclasses import dataclass

from .helpers import (
    IntercomAPIClient,
    IntercomCredentialsAccessToken,
    IntercomCredentialsOAuth,
    PaginationType,
    build_incremental_query,
    transform_company,
    transform_contact,
    transform_conversation,
)
from .settings import API_VERSION, REGIONAL_ENDPOINTS


class TestIntercomCredentials(unittest.TestCase):
    """Test credential classes."""

    def test_access_token_credentials(self):
        """Test access token credentials initialization."""
        creds = IntercomCredentialsAccessToken(
            access_token="test_token",
            region="eu"
        )
        self.assertEqual(creds.access_token, "test_token")
        self.assertEqual(creds.region, "eu")
        self.assertEqual(creds.base_url, REGIONAL_ENDPOINTS["eu"])

    def test_oauth_credentials(self):
        """Test OAuth credentials initialization."""
        creds = IntercomCredentialsOAuth(
            oauth_token="oauth_token",
            region="us"
        )
        self.assertEqual(creds.oauth_token, "oauth_token")
        self.assertEqual(creds.region, "us")
        self.assertEqual(creds.base_url, REGIONAL_ENDPOINTS["us"])

    def test_invalid_region(self):
        """Test that invalid region raises error."""
        with self.assertRaises(ValueError) as context:
            IntercomCredentialsAccessToken(
                access_token="test",
                region="invalid"
            )
        self.assertIn("Invalid region", str(context.exception))

    def test_default_region(self):
        """Test default region is US."""
        creds = IntercomCredentialsAccessToken(access_token="test")
        self.assertEqual(creds.region, "us")
        self.assertEqual(creds.base_url, REGIONAL_ENDPOINTS["us"])


class TestIntercomAPIClient(unittest.TestCase):
    """Test Intercom API client."""

    def setUp(self):
        """Set up test fixtures."""
        self.creds = IntercomCredentialsAccessToken(
            access_token="test_token",
            region="us"
        )
        self.client = IntercomAPIClient(self.creds)

    def test_client_initialization_access_token(self):
        """Test client initialization with access token."""
        self.assertEqual(
            self.client.headers["Authorization"],
            "Bearer test_token"
        )
        self.assertEqual(
            self.client.headers["Intercom-Version"],
            API_VERSION
        )
        self.assertEqual(self.client.base_url, REGIONAL_ENDPOINTS["us"])

    def test_client_initialization_oauth(self):
        """Test client initialization with OAuth token."""
        oauth_creds = IntercomCredentialsOAuth(oauth_token="oauth_test")
        client = IntercomAPIClient(oauth_creds)
        self.assertEqual(
            client.headers["Authorization"],
            "Bearer oauth_test"
        )

    @patch("ingestr.src.intercom.helpers.client")
    def test_make_request_success(self, mock_client):
        """Test successful API request."""
        mock_response = Mock()
        mock_response.status_code = 200
        mock_response.json.return_value = {"data": "test"}
        mock_client.request.return_value = mock_response

        result = self.client._make_request("GET", "/contacts")
        
        self.assertEqual(result, {"data": "test"})
        mock_client.request.assert_called_once()

    @patch("ingestr.src.intercom.helpers.client")
    @patch("ingestr.src.intercom.helpers.time.sleep")
    def test_make_request_rate_limit_retry(self, mock_sleep, mock_client):
        """Test rate limit handling with retry."""
        # First response: rate limited
        mock_response_429 = Mock()
        mock_response_429.status_code = 429
        mock_response_429.headers = {"X-RateLimit-Reset": "1000000010"}
        
        # Second response: success
        mock_response_200 = Mock()
        mock_response_200.status_code = 200
        mock_response_200.json.return_value = {"data": "success"}
        
        mock_client.request.side_effect = [mock_response_429, mock_response_200]
        
        with patch("ingestr.src.intercom.helpers.time.time", return_value=1000000000):
            result = self.client._make_request("GET", "/contacts")
        
        self.assertEqual(result, {"data": "success"})
        self.assertEqual(mock_client.request.call_count, 2)
        mock_sleep.assert_called_once()

    @patch("ingestr.src.intercom.helpers.client")
    def test_get_pages_simple_pagination(self, mock_client):
        """Test simple (no) pagination."""
        mock_response = Mock()
        mock_response.status_code = 200
        mock_response.json.return_value = {"admins": [{"id": "1"}, {"id": "2"}]}
        mock_client.request.return_value = mock_response

        pages = list(self.client.get_pages(
            "/admins", "admins", PaginationType.SIMPLE
        ))
        
        self.assertEqual(len(pages), 1)
        self.assertEqual(pages[0], [{"id": "1"}, {"id": "2"}])

    @patch("ingestr.src.intercom.helpers.client")
    def test_get_pages_cursor_pagination(self, mock_client):
        """Test cursor-based pagination."""
        # First page
        mock_response_1 = Mock()
        mock_response_1.status_code = 200
        mock_response_1.json.return_value = {
            "data": [{"id": "1"}],
            "pages": {"next": {"starting_after": "cursor_1"}}
        }
        
        # Second page (last)
        mock_response_2 = Mock()
        mock_response_2.status_code = 200
        mock_response_2.json.return_value = {
            "data": [{"id": "2"}],
            "pages": {}
        }
        
        mock_client.request.side_effect = [mock_response_1, mock_response_2]
        
        pages = list(self.client.get_pages(
            "/contacts", "data", PaginationType.CURSOR
        ))
        
        self.assertEqual(len(pages), 2)
        self.assertEqual(pages[0], [{"id": "1"}])
        self.assertEqual(pages[1], [{"id": "2"}])

    def test_search_method(self):
        """Test search method builds correct query."""
        with patch.object(self.client, 'get_pages') as mock_get_pages:
            mock_get_pages.return_value = iter([[{"id": "1"}]])
            
            query = {"field": "email", "operator": "=", "value": "test@example.com"}
            list(self.client.search("contacts", query))
            
            mock_get_pages.assert_called_once_with(
                endpoint="/contacts/search",
                data_key="data",
                pagination_type=PaginationType.SEARCH,
                search_query={"query": query}
            )


class TestTransformFunctions(unittest.TestCase):
    """Test data transformation functions."""

    def test_transform_contact(self):
        """Test contact transformation."""
        raw_contact = {
            "id": "123",
            "email": "test@example.com",
            "location": {
                "country": "US",
                "region": "CA",
                "city": "San Francisco"
            },
            "companies": {
                "data": [
                    {"id": "comp_1"},
                    {"id": "comp_2"}
                ]
            }
        }
        
        transformed = transform_contact(raw_contact)
        
        self.assertEqual(transformed["id"], "123")
        self.assertEqual(transformed["email"], "test@example.com")
        self.assertEqual(transformed["location_country"], "US")
        self.assertEqual(transformed["location_region"], "CA")
        self.assertEqual(transformed["location_city"], "San Francisco")
        self.assertEqual(transformed["company_ids"], ["comp_1", "comp_2"])
        self.assertEqual(transformed["companies_count"], 2)
        self.assertIn("custom_attributes", transformed)

    def test_transform_contact_minimal(self):
        """Test contact transformation with minimal data."""
        raw_contact = {"id": "123", "email": "test@example.com"}
        transformed = transform_contact(raw_contact)
        
        self.assertEqual(transformed["id"], "123")
        self.assertEqual(transformed["email"], "test@example.com")
        self.assertIn("custom_attributes", transformed)
        self.assertEqual(transformed["custom_attributes"], {})

    def test_transform_company(self):
        """Test company transformation."""
        raw_company = {
            "id": "456",
            "name": "Test Corp",
            "plan": {
                "id": "plan_1",
                "name": "Enterprise"
            }
        }
        
        transformed = transform_company(raw_company)
        
        self.assertEqual(transformed["id"], "456")
        self.assertEqual(transformed["name"], "Test Corp")
        self.assertEqual(transformed["plan_id"], "plan_1")
        self.assertEqual(transformed["plan_name"], "Enterprise")
        self.assertIn("custom_attributes", transformed)

    def test_transform_conversation(self):
        """Test conversation transformation."""
        raw_conversation = {
            "id": "789",
            "state": "open",
            "statistics": {
                "first_contact_reply_at": 123456,
                "first_admin_reply_at": 123457,
                "median_admin_reply_time": 60
            },
            "conversation_parts": {
                "total_count": 5
            }
        }
        
        transformed = transform_conversation(raw_conversation)
        
        self.assertEqual(transformed["id"], "789")
        self.assertEqual(transformed["state"], "open")
        self.assertEqual(transformed["first_contact_reply_at"], 123456)
        self.assertEqual(transformed["first_admin_reply_at"], 123457)
        self.assertEqual(transformed["median_admin_reply_time"], 60)
        self.assertEqual(transformed["conversation_parts_count"], 5)

    def test_build_incremental_query_single_condition(self):
        """Test building incremental query with single condition."""
        query = build_incremental_query("updated_at", 1000000)
        
        self.assertEqual(query, {
            "field": "updated_at",
            "operator": ">=",
            "value": 1000000
        })

    def test_build_incremental_query_range(self):
        """Test building incremental query with range."""
        query = build_incremental_query("updated_at", 1000000, 2000000)
        
        self.assertEqual(query["operator"], "AND")
        self.assertEqual(len(query["value"]), 2)
        self.assertEqual(query["value"][0], {
            "field": "updated_at",
            "operator": ">=",
            "value": 1000000
        })
        self.assertEqual(query["value"][1], {
            "field": "updated_at",
            "operator": "<=",
            "value": 2000000
        })


if __name__ == "__main__":
    unittest.main()
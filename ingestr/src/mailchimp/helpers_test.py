"""
Unit tests for Mailchimp helper functions.
"""

import unittest
from unittest.mock import Mock

from .helpers import (
    create_merge_resource,
    create_nested_resource,
    create_replace_resource,
    fetch_paginated,
)


class TestFetchPaginated(unittest.TestCase):
    """Test fetch_paginated helper function."""

    def test_fetch_paginated_with_data_key(self):
        """Test fetching paginated data with data_key."""
        # Mock session and response
        mock_session = Mock()
        mock_response_1 = Mock()
        mock_response_1.json.return_value = {
            "lists": [{"id": "1", "name": "List 1"}, {"id": "2", "name": "List 2"}]
        }
        mock_response_1.raise_for_status = Mock()

        mock_session.get.return_value = mock_response_1

        # Test fetching
        items = list(
            fetch_paginated(
                mock_session,
                "https://us10.api.mailchimp.com/3.0/lists",
                ("anystring", "test_key"),
                data_key="lists",
            )
        )

        # Verify results
        self.assertEqual(len(items), 2)
        self.assertEqual(items[0]["id"], "1")
        self.assertEqual(items[1]["id"], "2")

        # Verify pagination params
        call_args_list = mock_session.get.call_args_list
        self.assertEqual(call_args_list[0][1]["params"]["count"], 1000)
        self.assertEqual(call_args_list[0][1]["params"]["offset"], 0)

    def test_fetch_paginated_without_data_key(self):
        """Test fetching paginated data without data_key."""
        mock_session = Mock()
        mock_response = Mock()
        mock_response.json.return_value = {"id": "1", "account_name": "Test"}
        mock_response.raise_for_status = Mock()

        mock_session.get.return_value = mock_response

        # Test fetching
        items = list(
            fetch_paginated(
                mock_session,
                "https://us10.api.mailchimp.com/3.0/",
                ("anystring", "test_key"),
                data_key=None,
            )
        )

        # Verify results
        self.assertEqual(len(items), 1)
        self.assertEqual(items[0]["id"], "1")
        self.assertEqual(items[0]["account_name"], "Test")

    def test_fetch_paginated_list_response(self):
        """Test fetching when response is a list."""
        mock_session = Mock()
        mock_response = Mock()
        mock_response.json.return_value = [{"id": "1"}, {"id": "2"}]
        mock_response.raise_for_status = Mock()

        mock_session.get.return_value = mock_response

        # Test fetching
        items = list(
            fetch_paginated(
                mock_session,
                "https://us10.api.mailchimp.com/3.0/endpoint",
                ("anystring", "test_key"),
                data_key=None,
            )
        )

        # Verify results
        self.assertEqual(len(items), 2)
        self.assertEqual(items[0]["id"], "1")

    def test_fetch_paginated_stops_on_partial_page(self):
        """Test pagination stops when fewer items than count are returned."""
        mock_session = Mock()
        mock_response = Mock()
        mock_response.json.return_value = {
            "campaigns": [{"id": str(i)} for i in range(500)]
        }
        mock_response.raise_for_status = Mock()

        mock_session.get.return_value = mock_response

        # Test fetching
        items = list(
            fetch_paginated(
                mock_session,
                "https://us10.api.mailchimp.com/3.0/campaigns",
                ("anystring", "test_key"),
                data_key="campaigns",
            )
        )

        # Should only make one request since 500 < 1000
        self.assertEqual(len(items), 500)
        self.assertEqual(mock_session.get.call_count, 1)


class TestCreateReplaceResource(unittest.TestCase):
    """Test create_replace_resource helper function."""

    def test_create_replace_resource_with_pk(self):
        """Test creating replace resource with primary key."""
        mock_session = Mock()
        mock_response = Mock()
        mock_response.json.return_value = {"apps": [{"id": "1"}, {"id": "2"}]}
        mock_response.raise_for_status = Mock()
        mock_session.get.return_value = mock_response

        resource = create_replace_resource(
            base_url="https://us10.api.mailchimp.com/3.0",
            session=mock_session,
            auth=("anystring", "test_key"),
            name="authorized_apps",
            path="authorized-apps",
            key="apps",
            pk="id",
        )

        # Verify resource is created
        self.assertIsNotNone(resource)
        self.assertTrue(callable(resource))

    def test_create_replace_resource_without_pk(self):
        """Test creating replace resource without primary key."""
        mock_session = Mock()
        mock_response = Mock()
        mock_response.json.return_value = {"batches": []}
        mock_response.raise_for_status = Mock()
        mock_session.get.return_value = mock_response

        resource = create_replace_resource(
            base_url="https://us10.api.mailchimp.com/3.0",
            session=mock_session,
            auth=("anystring", "test_key"),
            name="batches",
            path="batches",
            key="batches",
            pk=None,
        )

        # Verify resource is created
        self.assertIsNotNone(resource)
        self.assertTrue(callable(resource))


class TestCreateMergeResource(unittest.TestCase):
    """Test create_merge_resource helper function."""

    def test_create_merge_resource(self):
        """Test creating merge resource with incremental loading."""
        mock_session = Mock()
        mock_response = Mock()
        mock_response.json.return_value = {
            "campaigns": [
                {"id": "1", "create_time": "2024-01-01"},
                {"id": "2", "create_time": "2024-01-02"},
            ]
        }
        mock_response.raise_for_status = Mock()
        mock_session.get.return_value = mock_response

        resource = create_merge_resource(
            base_url="https://us10.api.mailchimp.com/3.0",
            session=mock_session,
            auth=("anystring", "test_key"),
            name="campaigns",
            path="campaigns",
            key="campaigns",
            pk="id",
            ik="create_time",
        )

        # Verify resource is created
        self.assertIsNotNone(resource)
        self.assertTrue(callable(resource))


class TestCreateNestedResource(unittest.TestCase):
    """Test create_nested_resource helper function."""

    def test_create_nested_resource_with_data_key(self):
        """Test creating nested resource with data_key."""
        mock_session = Mock()

        # Mock responses
        def mock_get_side_effect(url, *args, **kwargs):
            response = Mock()
            response.raise_for_status = Mock()

            # Parent request
            if "domain-performance" not in url:
                response.json.return_value = {
                    "reports": [
                        {"id": "campaign_1", "send_time": "2024-01-01"},
                    ]
                }
            # Nested request for campaign_1
            elif "campaign_1" in url:
                response.json.return_value = {
                    "domains": [
                        {"domain": "gmail.com", "emails_sent": 100},
                        {"domain": "yahoo.com", "emails_sent": 50},
                    ]
                }
            else:
                response.json.return_value = {"reports": []}

            return response

        mock_session.get.side_effect = mock_get_side_effect

        resource = create_nested_resource(
            base_url="https://us10.api.mailchimp.com/3.0",
            session=mock_session,
            auth=("anystring", "test_key"),
            parent_resource_name="reports",
            parent_path="reports",
            parent_key="reports",
            parent_id_field="id",
            nested_name="reports_domain_performance",
            nested_path="reports/{id}/domain-performance",
            nested_key="domains",
            pk=None,
        )

        # Verify resource is created
        self.assertIsNotNone(resource)
        self.assertTrue(callable(resource))

    def test_create_nested_resource_without_data_key(self):
        """Test creating nested resource without data_key (whole response)."""
        mock_session = Mock()

        # Mock responses
        def mock_get_side_effect(url, *args, **kwargs):
            response = Mock()
            response.raise_for_status = Mock()

            # Parent request
            if "advice" not in url:
                response.json.return_value = {
                    "reports": [{"id": "campaign_1", "send_time": "2024-01-01"}]
                }
            # Nested request for campaign_1
            elif "campaign_1" in url:
                response.json.return_value = {
                    "type": "positive",
                    "message": "Campaign is performing well",
                }
            else:
                response.json.return_value = {"reports": []}

            return response

        mock_session.get.side_effect = mock_get_side_effect

        resource = create_nested_resource(
            base_url="https://us10.api.mailchimp.com/3.0",
            session=mock_session,
            auth=("anystring", "test_key"),
            parent_resource_name="reports",
            parent_path="reports",
            parent_key="reports",
            parent_id_field="id",
            nested_name="reports_advice",
            nested_path="reports/{id}/advice",
            nested_key=None,
            pk=None,
        )

        # Verify resource is created
        self.assertIsNotNone(resource)
        self.assertTrue(callable(resource))


if __name__ == "__main__":
    unittest.main()

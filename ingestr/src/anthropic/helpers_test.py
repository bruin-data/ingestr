"""Tests for Anthropic source helpers."""

import unittest
from unittest.mock import Mock, patch

from ingestr.src.anthropic.helpers import (
    fetch_api_keys,
    fetch_claude_code_usage,
    fetch_invites,
    fetch_organization_info,
    fetch_users,
    fetch_workspace_members,
    fetch_workspaces,
    flatten_usage_record,
)


class TestAnthropicHelpers(unittest.TestCase):
    def test_flatten_usage_record_user_actor(self):
        """Test flattening a usage record with user actor."""
        record = {
            "date": "2025-09-01T00:00:00Z",
            "actor": {
                "type": "user_actor",
                "email_address": "developer@company.com",
            },
            "organization_id": "dc9f6c26-b22c-4831-8d01-0446bada88f1",
            "customer_type": "api",
            "terminal_type": "vscode",
            "core_metrics": {
                "num_sessions": 5,
                "lines_of_code": {
                    "added": 1543,
                    "removed": 892,
                },
                "commits_by_claude_code": 12,
                "pull_requests_by_claude_code": 2,
            },
            "tool_actions": {
                "edit_tool": {
                    "accepted": 45,
                    "rejected": 5,
                },
                "multi_edit_tool": {
                    "accepted": 12,
                    "rejected": 2,
                },
                "write_tool": {
                    "accepted": 8,
                    "rejected": 1,
                },
                "notebook_edit_tool": {
                    "accepted": 3,
                    "rejected": 0,
                },
            },
            "model_breakdown": [
                {
                    "model": "claude-3-5-sonnet-20241022",
                    "tokens": {
                        "input": 100000,
                        "output": 35000,
                        "cache_read": 10000,
                        "cache_creation": 5000,
                    },
                    "estimated_cost": {
                        "currency": "USD",
                        "amount": 1025,
                    },
                }
            ],
        }

        flattened = flatten_usage_record(record)

        self.assertEqual(flattened["date"], "2025-09-01T00:00:00Z")
        self.assertEqual(flattened["actor_type"], "user_actor")
        self.assertEqual(flattened["actor_id"], "developer@company.com")
        self.assertEqual(
            flattened["organization_id"], "dc9f6c26-b22c-4831-8d01-0446bada88f1"
        )
        self.assertEqual(flattened["customer_type"], "api")
        self.assertEqual(flattened["terminal_type"], "vscode")
        self.assertEqual(flattened["num_sessions"], 5)
        self.assertEqual(flattened["lines_added"], 1543)
        self.assertEqual(flattened["lines_removed"], 892)
        self.assertEqual(flattened["commits_by_claude_code"], 12)
        self.assertEqual(flattened["pull_requests_by_claude_code"], 2)
        self.assertEqual(flattened["edit_tool_accepted"], 45)
        self.assertEqual(flattened["edit_tool_rejected"], 5)
        self.assertEqual(flattened["multi_edit_tool_accepted"], 12)
        self.assertEqual(flattened["multi_edit_tool_rejected"], 2)
        self.assertEqual(flattened["write_tool_accepted"], 8)
        self.assertEqual(flattened["write_tool_rejected"], 1)
        self.assertEqual(flattened["notebook_edit_tool_accepted"], 3)
        self.assertEqual(flattened["notebook_edit_tool_rejected"], 0)
        self.assertEqual(flattened["total_input_tokens"], 100000)
        self.assertEqual(flattened["total_output_tokens"], 35000)
        self.assertEqual(flattened["total_cache_read_tokens"], 10000)
        self.assertEqual(flattened["total_cache_creation_tokens"], 5000)
        self.assertEqual(flattened["total_estimated_cost_cents"], 1025)
        self.assertEqual(flattened["models_used"], "claude-3-5-sonnet-20241022")

    def test_flatten_usage_record_api_actor(self):
        """Test flattening a usage record with API actor."""
        record = {
            "date": "2025-09-01T00:00:00Z",
            "actor": {
                "type": "api_actor",
                "api_key_name": "production-key",
            },
            "organization_id": "dc9f6c26-b22c-4831-8d01-0446bada88f1",
            "customer_type": "subscription",
            "terminal_type": "iTerm.app",
            "core_metrics": {
                "num_sessions": 2,
                "lines_of_code": {
                    "added": 500,
                    "removed": 100,
                },
                "commits_by_claude_code": 5,
                "pull_requests_by_claude_code": 1,
            },
            "tool_actions": {},
            "model_breakdown": [],
        }

        flattened = flatten_usage_record(record)

        self.assertEqual(flattened["actor_type"], "api_actor")
        self.assertEqual(flattened["actor_id"], "production-key")
        self.assertEqual(flattened["customer_type"], "subscription")
        self.assertEqual(flattened["lines_added"], 500)
        self.assertEqual(flattened["lines_removed"], 100)
        self.assertEqual(flattened["total_input_tokens"], 0)
        self.assertEqual(flattened["total_estimated_cost_cents"], 0)
        self.assertIsNone(flattened["models_used"])

    def test_flatten_usage_record_multiple_models(self):
        """Test flattening with multiple models aggregates correctly."""
        record = {
            "date": "2025-09-01T00:00:00Z",
            "actor": {
                "type": "user_actor",
                "email_address": "test@example.com",
            },
            "organization_id": "test-org",
            "customer_type": "api",
            "terminal_type": "vscode",
            "core_metrics": {
                "num_sessions": 1,
                "lines_of_code": {"added": 0, "removed": 0},
                "commits_by_claude_code": 0,
                "pull_requests_by_claude_code": 0,
            },
            "tool_actions": {},
            "model_breakdown": [
                {
                    "model": "claude-3-5-sonnet-20241022",
                    "tokens": {
                        "input": 1000,
                        "output": 500,
                        "cache_read": 100,
                        "cache_creation": 50,
                    },
                    "estimated_cost": {
                        "currency": "USD",
                        "amount": 100,
                    },
                },
                {
                    "model": "claude-3-opus-20240229",
                    "tokens": {
                        "input": 2000,
                        "output": 1000,
                        "cache_read": 200,
                        "cache_creation": 100,
                    },
                    "estimated_cost": {
                        "currency": "USD",
                        "amount": 200,
                    },
                },
            ],
        }

        flattened = flatten_usage_record(record)

        self.assertEqual(flattened["total_input_tokens"], 3000)
        self.assertEqual(flattened["total_output_tokens"], 1500)
        self.assertEqual(flattened["total_cache_read_tokens"], 300)
        self.assertEqual(flattened["total_cache_creation_tokens"], 150)
        self.assertEqual(flattened["total_estimated_cost_cents"], 300)
        self.assertEqual(
            flattened["models_used"],
            "claude-3-5-sonnet-20241022,claude-3-opus-20240229",
        )

    @patch("ingestr.src.anthropic.helpers.AnthropicClient.get")
    def test_fetch_claude_code_usage_success(self, mock_get):
        """Test successful API fetch."""
        mock_response = Mock()
        mock_response.status_code = 200
        mock_response.json.return_value = {
            "data": [
                {
                    "date": "2025-09-01T00:00:00Z",
                    "actor": {
                        "type": "user_actor",
                        "email_address": "test@example.com",
                    },
                    "organization_id": "test-org",
                    "customer_type": "api",
                    "terminal_type": "vscode",
                    "core_metrics": {
                        "num_sessions": 1,
                        "lines_of_code": {"added": 100, "removed": 50},
                        "commits_by_claude_code": 2,
                        "pull_requests_by_claude_code": 1,
                    },
                    "tool_actions": {},
                    "model_breakdown": [],
                }
            ],
            "has_more": False,
            "next_page": None,
        }
        mock_get.return_value = mock_response

        results = list(fetch_claude_code_usage("sk-ant-admin-test", "2025-09-01"))

        self.assertEqual(len(results), 1)
        self.assertEqual(results[0]["actor_id"], "test@example.com")
        self.assertEqual(results[0]["lines_added"], 100)

        # Verify API call
        mock_get.assert_called_once()
        call_args = mock_get.call_args
        self.assertEqual(call_args[0][0], "organizations/usage_report/claude_code")
        # params is the second positional argument
        params = (
            call_args[0][1] if len(call_args[0]) > 1 else call_args[1].get("params", {})
        )
        self.assertEqual(params["starting_at"], "2025-09-01")
        self.assertEqual(params["ending_at"], "2025-09-01")

    @patch("ingestr.src.anthropic.helpers.AnthropicClient.get")
    def test_fetch_claude_code_usage_pagination(self, mock_get):
        """Test API pagination handling."""
        # First page response
        response1 = Mock()
        response1.status_code = 200
        response1.json.return_value = {
            "data": [
                {
                    "date": "2025-09-01T00:00:00Z",
                    "actor": {
                        "type": "user_actor",
                        "email_address": "user1@example.com",
                    },
                    "organization_id": "test-org",
                    "customer_type": "api",
                    "terminal_type": "vscode",
                    "core_metrics": {
                        "num_sessions": 1,
                        "lines_of_code": {"added": 100, "removed": 50},
                        "commits_by_claude_code": 0,
                        "pull_requests_by_claude_code": 0,
                    },
                    "tool_actions": {},
                    "model_breakdown": [],
                }
            ],
            "has_more": True,
            "next_page": "page_cursor_123",
        }

        # Second page response
        response2 = Mock()
        response2.status_code = 200
        response2.json.return_value = {
            "data": [
                {
                    "date": "2025-09-01T00:00:00Z",
                    "actor": {
                        "type": "user_actor",
                        "email_address": "user2@example.com",
                    },
                    "organization_id": "test-org",
                    "customer_type": "api",
                    "terminal_type": "vscode",
                    "core_metrics": {
                        "num_sessions": 2,
                        "lines_of_code": {"added": 200, "removed": 100},
                        "commits_by_claude_code": 0,
                        "pull_requests_by_claude_code": 0,
                    },
                    "tool_actions": {},
                    "model_breakdown": [],
                }
            ],
            "has_more": False,
            "next_page": None,
        }

        mock_get.side_effect = [response1, response2]

        results = list(fetch_claude_code_usage("sk-ant-admin-test", "2025-09-01"))

        self.assertEqual(len(results), 2)
        self.assertEqual(results[0]["actor_id"], "user1@example.com")
        self.assertEqual(results[1]["actor_id"], "user2@example.com")

        # Verify both API calls
        self.assertEqual(mock_get.call_count, 2)

    @patch("ingestr.src.anthropic.helpers.AnthropicClient.get")
    def test_fetch_claude_code_usage_auth_error(self, mock_get):
        """Test handling of authentication error."""
        from requests.exceptions import HTTPError

        mock_response = Mock()
        mock_response.status_code = 401
        http_error = HTTPError(response=mock_response)
        mock_response.raise_for_status.side_effect = http_error
        mock_get.return_value = mock_response

        with self.assertRaises(ValueError) as context:
            list(fetch_claude_code_usage("invalid-key", "2025-09-01"))

        self.assertIn("Invalid API key", str(context.exception))

    @patch("ingestr.src.anthropic.helpers.AnthropicClient.get")
    def test_fetch_claude_code_usage_no_data(self, mock_get):
        """Test handling when no data is available for a date."""
        from requests.exceptions import HTTPError

        mock_response = Mock()
        mock_response.status_code = 404
        http_error = HTTPError(response=mock_response)
        mock_response.raise_for_status.side_effect = http_error
        mock_get.return_value = mock_response

        results = list(fetch_claude_code_usage("sk-ant-admin-test", "2025-09-01"))

        self.assertEqual(len(results), 0)

    @patch("ingestr.src.anthropic.helpers.AnthropicClient.get")
    def test_fetch_organization_info(self, mock_get):
        """Test fetching organization information."""
        mock_response = Mock()
        mock_response.status_code = 200
        mock_response.json.return_value = {
            "id": "org-123",
            "name": "Test Organization",
            "settings": {"max_api_keys": 100},
            "created_at": "2023-01-01T00:00:00Z",
        }
        mock_get.return_value = mock_response

        result = fetch_organization_info("sk-ant-admin-test")

        self.assertEqual(result["id"], "org-123")
        self.assertEqual(result["name"], "Test Organization")
        mock_get.assert_called_once_with("organizations/me")

    @patch("ingestr.src.anthropic.helpers.AnthropicClient.get")
    def test_fetch_workspaces(self, mock_get):
        """Test fetching workspaces with pagination."""
        # First page
        response1 = Mock()
        response1.status_code = 200
        response1.json.return_value = {
            "data": [
                {
                    "id": "ws-1",
                    "name": "Workspace 1",
                    "type": "default",
                    "created_at": "2023-01-01T00:00:00Z",
                }
            ],
            "has_more": True,
            "next_page": "cursor-1",
        }

        # Second page
        response2 = Mock()
        response2.status_code = 200
        response2.json.return_value = {
            "data": [
                {
                    "id": "ws-2",
                    "name": "Workspace 2",
                    "type": "custom",
                    "created_at": "2023-02-01T00:00:00Z",
                }
            ],
            "has_more": False,
            "next_page": None,
        }

        mock_get.side_effect = [response1, response2]

        results = list(fetch_workspaces("sk-ant-admin-test"))

        self.assertEqual(len(results), 2)
        self.assertEqual(results[0]["id"], "ws-1")
        self.assertEqual(results[1]["id"], "ws-2")
        self.assertEqual(mock_get.call_count, 2)

    @patch("ingestr.src.anthropic.helpers.AnthropicClient.get")
    def test_fetch_api_keys(self, mock_get):
        """Test fetching API keys."""
        mock_response = Mock()
        mock_response.status_code = 200
        mock_response.json.return_value = {
            "data": [
                {
                    "id": "key-1",
                    "name": "Production Key",
                    "status": "active",
                    "created_at": "2023-01-01T00:00:00Z",
                    "workspace_id": "ws-1",
                }
            ],
            "has_more": False,
            "next_page": None,
        }
        mock_get.return_value = mock_response

        results = list(fetch_api_keys("sk-ant-admin-test"))

        self.assertEqual(len(results), 1)
        self.assertEqual(results[0]["id"], "key-1")
        self.assertEqual(results[0]["name"], "Production Key")

    @patch("ingestr.src.anthropic.helpers.AnthropicClient.get")
    def test_fetch_invites(self, mock_get):
        """Test fetching invites."""
        mock_response = Mock()
        mock_response.status_code = 200
        mock_response.json.return_value = {
            "data": [
                {
                    "id": "invite-1",
                    "email": "user@example.com",
                    "role": "member",
                    "expires_at": "2025-01-01T00:00:00Z",
                    "workspace_ids": ["ws-1"],
                }
            ],
            "has_more": False,
            "next_page": None,
        }
        mock_get.return_value = mock_response

        results = list(fetch_invites("sk-ant-admin-test"))

        self.assertEqual(len(results), 1)
        self.assertEqual(results[0]["email"], "user@example.com")

    @patch("ingestr.src.anthropic.helpers.AnthropicClient.get")
    def test_fetch_users(self, mock_get):
        """Test fetching users."""
        mock_response = Mock()
        mock_response.status_code = 200
        mock_response.json.return_value = {
            "data": [
                {
                    "id": "user-1",
                    "email": "admin@example.com",
                    "name": "Admin User",
                    "role": "admin",
                    "created_at": "2023-01-01T00:00:00Z",
                }
            ],
            "has_more": False,
            "next_page": None,
        }
        mock_get.return_value = mock_response

        results = list(fetch_users("sk-ant-admin-test"))

        self.assertEqual(len(results), 1)
        self.assertEqual(results[0]["email"], "admin@example.com")

    @patch("ingestr.src.anthropic.helpers.AnthropicClient.get")
    def test_fetch_workspace_members(self, mock_get):
        """Test fetching workspace members."""
        mock_response = Mock()
        mock_response.status_code = 200
        mock_response.json.return_value = {
            "data": [
                {
                    "workspace_id": "ws-1",
                    "user_id": "user-1",
                    "role": "member",
                    "added_at": "2023-01-01T00:00:00Z",
                }
            ],
            "has_more": False,
            "next_page": None,
        }
        mock_get.return_value = mock_response

        results = list(fetch_workspace_members("sk-ant-admin-test", "ws-1"))

        self.assertEqual(len(results), 1)
        self.assertEqual(results[0]["workspace_id"], "ws-1")
        self.assertEqual(results[0]["user_id"], "user-1")
        mock_get.assert_called_once()
        call_args = mock_get.call_args
        # Check that workspace_id is in the params
        self.assertEqual(call_args[0][0], "workspace_members")
        params = (
            call_args[0][1] if len(call_args[0]) > 1 else call_args[1].get("params", {})
        )
        self.assertEqual(params["workspace_id"], "ws-1")


if __name__ == "__main__":
    unittest.main()

#!/usr/bin/env python3
"""
Test helpers.py - verifies fetch_documents returns all fields, not just id.
"""

import unittest
from unittest.mock import MagicMock

from ingestr.src.couchbase_source.helpers import fetch_documents


class TestFetchDocuments(unittest.TestCase):
    """Test fetch_documents function."""

    def test_returns_all_fields_not_just_id(self):
        """Critical test: verify all fields are returned, not just id."""
        # Mock cluster
        mock_cluster = MagicMock()

        # Mock query result with multiple fields
        mock_result = [
            {
                "id": "airport_1",
                "name": "San Francisco International",
                "code": "SFO",
                "city": "San Francisco",
                "country": "USA",
            },
            {
                "id": "airport_2",
                "name": "Los Angeles International",
                "code": "LAX",
                "city": "Los Angeles",
                "country": "USA",
            }
        ]
        mock_cluster.query.return_value = iter(mock_result)

        # Fetch documents
        docs = list(fetch_documents(
            cluster=mock_cluster,
            bucket_name="test",
            scope_name="scope",
            collection_name="collection",
            incremental=None,
            limit=None,
        ))

        # CRITICAL: Verify we got ALL fields, not just id
        self.assertEqual(len(docs), 2)

        # Check first document has all fields
        first = docs[0]
        self.assertEqual(first["id"], "airport_1")
        self.assertEqual(first["name"], "San Francisco International")
        self.assertEqual(first["code"], "SFO")
        self.assertEqual(first["city"], "San Francisco")
        self.assertEqual(first["country"], "USA")

        # Check second document
        second = docs[1]
        self.assertEqual(second["id"], "airport_2")
        self.assertEqual(second["code"], "LAX")

    def test_query_uses_alias_format(self):
        """Test query uses 'c.*' format (not full path)."""
        mock_cluster = MagicMock()
        mock_cluster.query.return_value = iter([])

        list(fetch_documents(
            cluster=mock_cluster,
            bucket_name="bucket",
            scope_name="scope",
            collection_name="collection",
            incremental=None,
            limit=None,
        ))

        # Get the query that was called
        query = mock_cluster.query.call_args[0][0]

        # CRITICAL: Should use "c.*" not "bucket.scope.collection.*"
        self.assertIn("c.*", query)
        self.assertIn("FROM `bucket`.`scope`.`collection` c", query)

        # Should NOT have the full path in SELECT
        self.assertNotIn("`bucket`.`scope`.`collection`.*", query)

    def test_limit_parameter(self):
        """Test limit is applied to query."""
        mock_cluster = MagicMock()
        mock_cluster.query.return_value = iter([])

        list(fetch_documents(
            cluster=mock_cluster,
            bucket_name="bucket",
            scope_name="scope",
            collection_name="collection",
            incremental=None,
            limit=10,
        ))

        query = mock_cluster.query.call_args[0][0]
        self.assertIn("LIMIT 10", query)

    def test_no_limit_by_default(self):
        """Test no limit when limit=None."""
        mock_cluster = MagicMock()
        mock_cluster.query.return_value = iter([])

        list(fetch_documents(
            cluster=mock_cluster,
            bucket_name="bucket",
            scope_name="scope",
            collection_name="collection",
            incremental=None,
            limit=None,
        ))

        query = mock_cluster.query.call_args[0][0]
        self.assertNotIn("LIMIT", query)

    def test_meta_id_selected(self):
        """Test META().id is selected as id field."""
        mock_cluster = MagicMock()
        mock_cluster.query.return_value = iter([])

        list(fetch_documents(
            cluster=mock_cluster,
            bucket_name="bucket",
            scope_name="scope",
            collection_name="collection",
            incremental=None,
            limit=None,
        ))

        query = mock_cluster.query.call_args[0][0]
        self.assertIn("META().id as id", query)

    def test_empty_result(self):
        """Test handles empty result gracefully."""
        mock_cluster = MagicMock()
        mock_cluster.query.return_value = iter([])

        docs = list(fetch_documents(
            cluster=mock_cluster,
            bucket_name="bucket",
            scope_name="scope",
            collection_name="collection",
            incremental=None,
            limit=None,
        ))

        self.assertEqual(len(docs), 0)


if __name__ == "__main__":
    unittest.main()

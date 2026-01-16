import unittest
from unittest.mock import patch

import dlt
import pendulum
import pytest
from dlt.sources.credentials import ConnectionStringCredentials

from ingestr.src import sources
from ingestr.src.sources import AdjustSource, FluxxSource, MongoDbSource, SqlSource


class SqlSourceTest(unittest.TestCase):
    def test_sql_source_requires_two_fields_in_table(self):
        source = SqlSource()
        with pytest.raises(ValueError):
            uri = "bigquery://my-project"
            source.dlt_source(uri, "onetable")

    def test_table_instance_is_created(self):
        uri = "bigquery://my-project"
        table = "schema.table"

        # monkey patch the sql_table function
        def sql_table(
            credentials: ConnectionStringCredentials,
            schema,
            table,
            incremental,
            backend,
            chunk_size,
            **kwargs,
        ):
            self.assertEqual(str(credentials.to_url()), uri)
            self.assertEqual(schema, "schema")
            self.assertEqual(table, "table")
            self.assertEqual(backend, "sqlalchemy")
            self.assertIsNone(incremental)
            return dlt.resource()

        source = SqlSource(table_builder=sql_table)
        res = source.dlt_source(uri, table)
        self.assertIsNotNone(res)

    def test_table_instance_is_created_with_incremental(self):
        uri = "bigquery://my-project"
        table = "schema.table"
        incremental_key = "id"

        # monkey patch the sql_table function
        def sql_table(
            credentials: ConnectionStringCredentials,
            schema,
            table,
            incremental,
            backend,
            chunk_size,
            **kwargs,
        ):
            self.assertEqual(str(credentials.to_url()), uri)
            self.assertEqual(schema, "schema")
            self.assertEqual(table, "table")
            self.assertEqual(backend, "sqlalchemy")
            self.assertIsInstance(incremental, dlt.sources.incremental)
            self.assertEqual(incremental.cursor_path, incremental_key)
            return dlt.resource()

        source = SqlSource(table_builder=sql_table)
        res = source.dlt_source(uri, table, incremental_key=incremental_key)
        self.assertIsNotNone(res)

    @patch("ingestr.src.destinations.get_databricks_oauth_token")
    def test_databricks_oauth_m2m_credentials(self, mock_get_token):
        """Test that Databricks client_id and client_secret trigger OAuth M2M flow"""
        mock_get_token.return_value = "mocked_access_token"

        uri = "databricks://@hostname?http_path=/sql/1.0/warehouses/abc&catalog=main&client_id=my_client_id&client_secret=my_secret"
        table = "schema.table"

        # Track the URI that gets passed to sql_table
        captured_uri = None

        def sql_table(
            credentials: ConnectionStringCredentials,
            schema,
            table,
            incremental,
            backend,
            chunk_size,
            **kwargs,
        ):
            nonlocal captured_uri
            captured_uri = str(credentials.to_url())
            return dlt.resource()

        source = SqlSource(table_builder=sql_table)
        source.dlt_source(uri, table)

        # Verify OAuth function was called with correct args
        mock_get_token.assert_called_once_with("hostname", "my_client_id", "my_secret")

        # Verify the URI was reconstructed with the access token
        self.assertIn("token:mocked_access_token@", captured_uri)
        # Verify client_id and client_secret were removed from query params
        self.assertNotIn("client_id", captured_uri)
        self.assertNotIn("client_secret", captured_uri)
        # Verify other query params are preserved
        self.assertIn("http_path", captured_uri)
        self.assertIn("catalog", captured_uri)

    def test_databricks_password_auth_skips_oauth(self):
        """Test that Databricks with password (token) in URI skips OAuth flow"""
        uri = "databricks://token:dapi123abc@hostname?http_path=/sql/1.0/warehouses/abc&catalog=main"
        table = "schema.table"

        # Track the URI that gets passed to sql_table
        captured_uri = None

        def sql_table(
            credentials: ConnectionStringCredentials,
            schema,
            table,
            incremental,
            backend,
            chunk_size,
            **kwargs,
        ):
            nonlocal captured_uri
            captured_uri = str(credentials.to_url())
            return dlt.resource()

        source = SqlSource(table_builder=sql_table)
        source.dlt_source(uri, table)

        # Verify the URI passes through unchanged (still has the original token)
        self.assertIn("dapi123abc", captured_uri)
        self.assertIn("hostname", captured_uri)


class MongoDbSourceTest(unittest.TestCase):
    def test_sql_source_requires_two_fields_in_table(self):
        source = MongoDbSource()
        with pytest.raises(ValueError):
            uri = "mongodb://my-project"
            source.dlt_source(uri, "onetable")

    def test_table_instance_is_created(self):
        uri = "mongodb://my-project"
        table = "schema.table"

        # monkey patch the mongo function
        def mongo(connection_url, database, collection, incremental, parallel):
            self.assertEqual(connection_url, uri)
            self.assertEqual(database, "schema")
            self.assertEqual(collection, "table")
            self.assertIsNone(incremental)
            return dlt.resource()

        source = MongoDbSource(table_builder=mongo)
        res = source.dlt_source(uri, table)
        self.assertIsNotNone(res)

    def test_table_instance_is_created_with_incremental(self):
        uri = "mongodb://my-project"
        table = "schema.table"
        incremental_key = "id"

        # monkey patch the mongo function
        def mongo(connection_url, database, collection, incremental, parallel):
            self.assertEqual(connection_url, uri)
            self.assertEqual(database, "schema")
            self.assertEqual(collection, "table")
            self.assertIsInstance(incremental, dlt.sources.incremental)
            self.assertEqual(incremental.cursor_path, incremental_key)
            return dlt.resource()

        source = MongoDbSource(table_builder=mongo)
        res = source.dlt_source(uri, table, incremental_key=incremental_key)
        self.assertIsNotNone(res)


class AdjustSourceTest(unittest.TestCase):
    def test_table_instance_is_created(self):
        uri = "adjust://?api_key=my-api-key"
        table = "creatives"

        # monkey patch the adjust function
        @dlt.source(max_table_nesting=0)
        def adjust(
            start_date, end_date, api_key, dimensions, metrics, merge_key, filters
        ):
            self.assertEqual(api_key, "my-api-key")
            self.assertIsNone(dimensions)
            self.assertIsNone(metrics)
            self.assertIsNone(merge_key)
            self.assertEqual(filters, [])

            # ensure the lookback days is 30
            self.assertEqual(
                start_date,
                pendulum.datetime(2024, 10, 6).replace(
                    hour=0, minute=0, second=0, microsecond=0
                ),
            )
            self.assertEqual(end_date, pendulum.datetime(2024, 11, 12))

            def creatives():
                return dlt.resource()

            return creatives

        sources.adjust_source = adjust
        source = AdjustSource()
        res = source.dlt_source(
            uri, table, interval_start="2024-11-05", interval_end="2024-11-12"
        )
        self.assertIsNotNone(res)

    def test_custom_table_with_dimensions_and_metrics(self):
        uri = "adjust://?api_key=my-api-key"
        table = "custom:hour,day:impressions,cost"

        # monkey patch the adjust function
        @dlt.source(max_table_nesting=0)
        def adjust(
            start_date, end_date, api_key, dimensions, metrics, merge_key, filters
        ):
            self.assertEqual(api_key, "my-api-key")
            self.assertEqual(dimensions, ["hour", "day"])
            self.assertEqual(metrics, ["impressions", "cost"])
            self.assertIsNone(merge_key)
            self.assertEqual(filters, [])

            def custom():
                return dlt.resource()

            return custom

        sources.adjust_source = adjust
        source = AdjustSource()
        res = source.dlt_source(uri, table)
        self.assertIsNotNone(res)

    def test_custom_table_with_dimensions_and_metrics_and_filters(self):
        uri = "adjust://?api_key=my-api-key"
        table = "custom:hour,day:impressions,cost:campaign=campaign1,campaign2,key1=value1,key2=value2"

        # monkey patch the adjust function
        @dlt.source(max_table_nesting=0)
        def adjust(
            start_date, end_date, api_key, dimensions, metrics, merge_key, filters
        ):
            self.assertEqual(api_key, "my-api-key")
            self.assertEqual(dimensions, ["hour", "day"])
            self.assertEqual(metrics, ["impressions", "cost"])
            self.assertIsNone(merge_key)
            self.assertEqual(
                filters,
                {
                    "campaign": ["campaign1", "campaign2"],
                    "key1": "value1",
                    "key2": "value2",
                },
            )

            def custom():
                return dlt.resource()

            return custom

        sources.adjust_source = adjust
        source = AdjustSource()
        res = source.dlt_source(uri, table)
        self.assertIsNotNone(res)


class FluxxSourceTest(unittest.TestCase):
    def test_rejects_uri_with_embedded_scheme(self):
        source = FluxxSource()

        uris = [
            "fluxx://https://acme.fluxx.io?client_id=xxx&client_secret=xxx",
            "fluxx://http://acme.fluxx.io?client_id=xxx&client_secret=xxx",
        ]

        for uri in uris:
            with self.assertRaises(ValueError) as exc_info:
                source.dlt_source(uri, "grant_request")

            self.assertIn("Invalid Fluxx URI format", str(exc_info.exception))


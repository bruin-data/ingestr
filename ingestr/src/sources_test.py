import base64
import json
import os
import tempfile
import unittest

import dlt
import pendulum
import pytest
from dlt.sources.credentials import ConnectionStringCredentials

from ingestr.src import sources
from ingestr.src.sources import AdjustSource, MongoDbSource, SqlSource


class SqlSourceTest(unittest.TestCase):
    def test_sql_source_requires_two_fields_in_table(self):
        source = SqlSource()
        with pytest.raises(ValueError):
            uri = "bigquery://my-project"
            source.dlt_source(uri, "onetable")

    def test_bigquery_source_requires_credentials_or_adc(self):
        # When no credentials are provided and use_adc is not set, should raise error
        source = SqlSource()
        with pytest.raises(
            ValueError, match="credentials_path or credentials_base64 is required"
        ):
            uri = "bigquery://my-project"
            source.dlt_source(uri, "schema.table")

    def test_bigquery_source_raises_error_for_invalid_base64_credentials(self):
        # Test that ValueError/UnicodeDecodeError is raised when base64 credentials are invalid
        source = SqlSource()
        uri = "bigquery://my-project?credentials_base64=invalid_base64!!"
        table = "schema.table"
        with pytest.raises((ValueError, UnicodeDecodeError)):
            source.dlt_source(uri, table)

    def test_bigquery_source_raises_error_for_invalid_json_in_base64_credentials(self):
        # Test that JSONDecodeError is raised when base64 decodes but isn't valid JSON
        source = SqlSource()
        invalid_json = base64.b64encode(b"not valid json").decode("utf-8")
        uri = f"bigquery://my-project?credentials_base64={invalid_json}"
        table = "schema.table"
        with pytest.raises(json.JSONDecodeError):
            source.dlt_source(uri, table)

    def test_bigquery_source_with_valid_credentials_path(self):
        # Test that credentials_path is passed correctly in connection string
        # Create a temporary valid credentials file
        with tempfile.NamedTemporaryFile(mode="w", suffix=".json", delete=False) as f:
            json.dump({"type": "service_account", "project_id": "test"}, f)
            temp_path = f.name

        try:
            uri = f"bigquery://my-project?credentials_path={temp_path}"
            table = "schema.table"

            def sql_table(
                credentials: ConnectionStringCredentials,
                schema,
                table,
                incremental,
                backend,
                chunk_size,
                **kwargs,
            ):
                # Verify credentials_path is in the connection string
                cred_url = str(credentials.to_url())
                self.assertIn("bigquery://my-project", cred_url)
                # Check that credentials_path parameter is present (path will be URL-encoded)
                self.assertIn("credentials_path=", cred_url)
                # Also verify the filename appears in the URL (even if encoded)
                filename = os.path.basename(temp_path)
                self.assertIn(filename, cred_url)
                self.assertEqual(schema, "schema")
                self.assertEqual(table, "table")
                return dlt.resource()

            source_with_builder = SqlSource(table_builder=sql_table)
            res = source_with_builder.dlt_source(uri, table)
            self.assertIsNotNone(res)
        finally:
            os.unlink(temp_path)


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

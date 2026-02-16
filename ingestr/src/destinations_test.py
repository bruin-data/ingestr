import json
import os
import unittest
from unittest.mock import patch

import dlt
import pytest

from ingestr.src.destinations import (
    BigQueryDestination,
    ClickhouseDestination,
    DatabricksDestination,
    DuckDBDestination,
    MsSQLDestination,
    PostgresDestination,
    RedshiftDestination,
    SnowflakeDestination,
)


class BigQueryDestinationTest(unittest.TestCase):
    destination = BigQueryDestination()
    abs_path_to_credentials = os.path.abspath(
        os.path.join(os.path.dirname(__file__), "./testdata/fakebqcredentials.json")
    )
    actual_credentials: dict = {}

    def setUp(self):
        with open(self.abs_path_to_credentials, "r") as f:
            self.actual_credentials = json.load(f)

    def test_bq_destination_simple_uri(self):
        uri = f"bigquery://my-project?credentials_path={self.abs_path_to_credentials}"
        result = self.destination.dlt_dest(uri)

        self.assertTrue(isinstance(result, dlt.destinations.bigquery))
        self.assertEqual(result.config_params["credentials"], self.actual_credentials)
        self.assertTrue("location" not in result.config_params)

    def test_bq_destination_with_location(self):
        uri = f"bigquery://my-project?credentials_path={self.abs_path_to_credentials}&location=EU"
        result = self.destination.dlt_dest(uri)

        self.assertTrue(isinstance(result, dlt.destinations.bigquery))
        self.assertEqual(result.config_params["credentials"], self.actual_credentials)
        self.assertEqual(result.config_params["location"], "EU")

    def test_bq_destination_run_params_require_two_or_three_fields(self):
        with pytest.raises(ValueError):
            self.destination.dlt_run_params("", "sometable")

        with pytest.raises(ValueError):
            self.destination.dlt_run_params("", "sometable.with.extra.fields")

    def test_bq_destination_run_params_parse_table_names_correctly(self):
        result = self.destination.dlt_run_params("", "dataset.sometable")
        self.assertEqual(result, {"dataset_name": "dataset", "table_name": "sometable"})

        result = self.destination.dlt_run_params("", "project.dataset.sometable")
        self.assertEqual(result, {"dataset_name": "dataset", "table_name": "sometable"})


class GenericSqlDestinationFixture(object):
    def test_credentials_are_passed_correctly(self):
        uri = "some-uri"
        result = self.destination.dlt_dest(uri)

        self.assertTrue(isinstance(result, self.expected_class))
        self.assertEqual(result.config_params["credentials"], uri)

    def test_destination_run_params_require_two_fields(self):
        with pytest.raises(ValueError):
            self.destination.dlt_run_params("", "sometable")

        with pytest.raises(ValueError):
            self.destination.dlt_run_params("", "sometable.with.extra")

    def test_destination_run_params_parse_table_names_correctly(self):
        result = self.destination.dlt_run_params("", "dataset.sometable")
        self.assertEqual(result, {"dataset_name": "dataset", "table_name": "sometable"})


class PostgresDestinationTest(unittest.TestCase, GenericSqlDestinationFixture):
    destination = PostgresDestination()
    expected_class = dlt.destinations.postgres


class SnowflakeDestinationTest(unittest.TestCase, GenericSqlDestinationFixture):
    destination = SnowflakeDestination()
    expected_class = dlt.destinations.snowflake


class RedshiftDestinationTest(unittest.TestCase, GenericSqlDestinationFixture):
    destination = RedshiftDestination()
    expected_class = dlt.destinations.redshift


class DuckDBDestinationTest(unittest.TestCase, GenericSqlDestinationFixture):
    destination = DuckDBDestination()
    expected_class = dlt.destinations.duckdb


class MsSQLDestinationTest(unittest.TestCase, GenericSqlDestinationFixture):
    destination = MsSQLDestination()
    expected_class = dlt.destinations.mssql


class DatabricksDestinationTest(unittest.TestCase):
    destination = DatabricksDestination()
    expected_class = dlt.destinations.databricks

    def test_credentials_are_passed_correctly(self):
        uri = (
            "databricks://token:password@hostname?http_path=/path/123&catalog=workspace"
        )
        result = self.destination.dlt_dest(uri)

        self.assertTrue(isinstance(result, self.expected_class))
        # Override the generic test - expect parsed credentials, not raw URI
        creds = result.config_params["credentials"]
        self.assertEqual(creds["access_token"], "password")
        self.assertEqual(creds["server_hostname"], "hostname")
        self.assertEqual(creds["http_path"], "/path/123")
        self.assertEqual(creds["catalog"], "workspace")

    def test_run_params_with_schema_dot_table(self):
        """Test that schema.table format works"""
        uri = (
            "databricks://token:password@hostname?http_path=/path/123&catalog=workspace"
        )
        result = self.destination.dlt_run_params(uri, "myschema.mytable")
        self.assertEqual(result, {"dataset_name": "myschema", "table_name": "mytable"})

    def test_run_params_with_schema_in_uri(self):
        """Test that just table name works when schema is in URI"""
        uri = "databricks://token:password@hostname?http_path=/path/123&catalog=workspace&schema=myschema"
        result = self.destination.dlt_run_params(uri, "mytable")
        self.assertEqual(result, {"dataset_name": "myschema", "table_name": "mytable"})

    def test_run_params_schema_dot_table_overrides_uri_schema(self):
        """Test that schema.table format overrides schema in URI"""
        uri = "databricks://token:password@hostname?http_path=/path/123&catalog=workspace&schema=old_schema"
        result = self.destination.dlt_run_params(uri, "new_schema.mytable")
        self.assertEqual(
            result, {"dataset_name": "new_schema", "table_name": "mytable"}
        )

    def test_run_params_requires_schema(self):
        """Test that error is raised when no schema is provided"""
        uri = (
            "databricks://token:password@hostname?http_path=/path/123&catalog=workspace"
        )
        with pytest.raises(ValueError) as exc_info:
            self.destination.dlt_run_params(uri, "mytable")
        self.assertIn("schema", str(exc_info.value).lower())

    @patch("ingestr.src.destinations.get_databricks_oauth_token")
    def test_oauth_m2m_credentials(self, mock_get_token):
        """Test that client_id and client_secret trigger OAuth M2M flow"""
        mock_get_token.return_value = "mocked_access_token"

        uri = "databricks://@hostname?http_path=/path/123&catalog=workspace&client_id=my_client_id&client_secret=my_secret"
        result = self.destination.dlt_dest(uri)

        mock_get_token.assert_called_once_with("hostname", "my_client_id", "my_secret")
        creds = result.config_params["credentials"]
        self.assertEqual(creds["access_token"], "mocked_access_token")
        self.assertEqual(creds["server_hostname"], "hostname")

    def test_missing_hostname_raises_error(self):
        """Test that missing hostname raises ValueError"""
        uri = "databricks://?http_path=/path/123&catalog=workspace"
        with pytest.raises(ValueError) as exc_info:
            self.destination.dlt_dest(uri)
        self.assertIn("hostname", str(exc_info.value).lower())

    def test_missing_token_and_oauth_raises_error(self):
        """Test that missing both token and OAuth credentials raises ValueError"""
        uri = "databricks://@hostname?http_path=/path/123&catalog=workspace"
        with pytest.raises(ValueError) as exc_info:
            self.destination.dlt_dest(uri)
        self.assertIn("access token", str(exc_info.value).lower())


class ClickhouseDestinationTest(unittest.TestCase):
    destination = ClickhouseDestination()

    def test_engine_settings_parsed_from_uri(self):
        uri = "clickhouse://user:pass@localhost:9000/mydb?secure=0&engine.index_granularity=8192&engine.storage_policy=default"
        self.assertEqual(
            self.destination.engine_settings(uri),
            {"index_granularity": "8192", "storage_policy": "default"},
        )

    def test_non_engine_params_excluded(self):
        uri = "clickhouse://user:pass@localhost:9000/mydb?secure=0&http_port=8123&engine.index_granularity=8192"
        settings = self.destination.engine_settings(uri)
        self.assertNotIn("secure", settings)
        self.assertNotIn("http_port", settings)
        self.assertEqual(settings, {"index_granularity": "8192"})

    def test_no_engine_settings_returns_empty_dict(self):
        uri = "clickhouse://user:pass@localhost:9000/mydb?secure=0"
        self.assertEqual(self.destination.engine_settings(uri), {})

    def test_engine_type_parsed_from_uri(self):
        uri = "clickhouse://user:pass@localhost:9000/mydb?secure=0&engine=shared_merge_tree"
        self.assertEqual(self.destination.engine_type(uri), "shared_merge_tree")

    def test_engine_type_returns_none_when_absent(self):
        uri = "clickhouse://user:pass@localhost:9000/mydb?secure=0"
        self.assertIsNone(self.destination.engine_type(uri))

    def test_engine_and_engine_settings_together(self):
        uri = "clickhouse://user:pass@localhost:9000/mydb?engine=merge_tree&engine.index_granularity=8192&engine.storage_policy=default"
        self.assertEqual(self.destination.engine_type(uri), "merge_tree")
        self.assertEqual(
            self.destination.engine_settings(uri),
            {"index_granularity": "8192", "storage_policy": "default"},
        )

    def test_engine_not_included_in_engine_settings(self):
        uri = "clickhouse://user:pass@localhost:9000/mydb?engine=shared_merge_tree&engine.index_granularity=8192"
        settings = self.destination.engine_settings(uri)
        self.assertNotIn("", settings)
        self.assertNotIn("shared_merge_tree", settings.values())
        self.assertEqual(settings, {"index_granularity": "8192"})

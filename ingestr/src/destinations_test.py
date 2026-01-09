import json
import os
import unittest
from unittest.mock import MagicMock, patch

import dlt
import pytest

from ingestr.src.destinations import (
    BigQueryDestination,
    DatabricksDestination,
    DuckDBDestination,
    MsSQLDestination,
    PostgresDestination,
    RedshiftDestination,
    SnowflakeDestination,
    get_databricks_oauth_token,
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
        """Test that OAuth M2M credentials (client_id/client_secret) work"""
        mock_get_token.return_value = "oauth_access_token_123"

        uri = "databricks://@hostname.cloud.databricks.com?http_path=/path/123&catalog=workspace&client_id=my_client_id&client_secret=my_client_secret"
        result = self.destination.dlt_dest(uri)

        self.assertTrue(isinstance(result, self.expected_class))
        creds = result.config_params["credentials"]
        self.assertEqual(creds["access_token"], "oauth_access_token_123")
        self.assertEqual(creds["server_hostname"], "hostname.cloud.databricks.com")
        self.assertEqual(creds["http_path"], "/path/123")
        self.assertEqual(creds["catalog"], "workspace")

        # Verify the OAuth token was fetched with correct parameters
        mock_get_token.assert_called_once_with(
            "hostname.cloud.databricks.com", "my_client_id", "my_client_secret"
        )

    def test_traditional_token_auth_still_works(self):
        """Test that traditional token auth continues to work when no client_id/client_secret"""
        uri = "databricks://token:my_access_token@hostname?http_path=/path/123&catalog=workspace"
        result = self.destination.dlt_dest(uri)

        self.assertTrue(isinstance(result, self.expected_class))
        creds = result.config_params["credentials"]
        self.assertEqual(creds["access_token"], "my_access_token")


class TestDatabricksOAuthToken(unittest.TestCase):
    @patch("ingestr.src.destinations.requests.post")
    def test_get_databricks_oauth_token_success(self, mock_post):
        """Test successful OAuth token retrieval"""
        mock_response = MagicMock()
        mock_response.status_code = 200
        mock_response.json.return_value = {"access_token": "test_token_abc"}
        mock_post.return_value = mock_response

        token = get_databricks_oauth_token(
            "dbc-xxx.cloud.databricks.com", "client_123", "secret_456"
        )

        self.assertEqual(token, "test_token_abc")
        mock_post.assert_called_once_with(
            "https://dbc-xxx.cloud.databricks.com/oidc/v1/token",
            data={
                "grant_type": "client_credentials",
                "scope": "all-apis",
            },
            auth=("client_123", "secret_456"),
            headers={"Content-Type": "application/x-www-form-urlencoded"},
        )

    @patch("ingestr.src.destinations.requests.post")
    def test_get_databricks_oauth_token_failure(self, mock_post):
        """Test OAuth token retrieval failure raises ValueError"""
        mock_response = MagicMock()
        mock_response.status_code = 401
        mock_response.text = "Unauthorized"
        mock_post.return_value = mock_response

        with pytest.raises(ValueError) as exc_info:
            get_databricks_oauth_token(
                "dbc-xxx.cloud.databricks.com", "bad_client", "bad_secret"
            )
        self.assertIn("Failed to obtain Databricks OAuth token", str(exc_info.value))
        self.assertIn("401", str(exc_info.value))

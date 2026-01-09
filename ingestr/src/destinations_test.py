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


class TestGetDatabricksOAuthToken(unittest.TestCase):
    @patch("ingestr.src.destinations.requests.post")
    def test_successful_token_exchange(self, mock_post):
        """Test successful OAuth token exchange"""
        mock_response = MagicMock()
        mock_response.status_code = 200
        mock_response.json.return_value = {"access_token": "test_token_123"}
        mock_response.raise_for_status = MagicMock()
        mock_post.return_value = mock_response

        token = get_databricks_oauth_token("hostname.com", "client_id", "client_secret")

        self.assertEqual(token, "test_token_123")
        mock_post.assert_called_once()
        call_args = mock_post.call_args
        self.assertEqual(call_args[0][0], "https://hostname.com/oidc/v1/token")
        self.assertEqual(call_args[1]["auth"], ("client_id", "client_secret"))

    def test_missing_server_hostname_raises_error(self):
        """Test that empty server_hostname raises ValueError"""
        with pytest.raises(ValueError) as exc_info:
            get_databricks_oauth_token("", "client_id", "client_secret")
        self.assertIn("server_hostname", str(exc_info.value))

    def test_missing_client_id_raises_error(self):
        """Test that empty client_id raises ValueError"""
        with pytest.raises(ValueError) as exc_info:
            get_databricks_oauth_token("hostname.com", "", "client_secret")
        self.assertIn("client_id", str(exc_info.value))

    def test_missing_client_secret_raises_error(self):
        """Test that empty client_secret raises ValueError"""
        with pytest.raises(ValueError) as exc_info:
            get_databricks_oauth_token("hostname.com", "client_id", "")
        self.assertIn("client_secret", str(exc_info.value))

    @patch("ingestr.src.destinations.requests.post")
    def test_http_error_raises_value_error(self, mock_post):
        """Test that HTTP errors are converted to ValueError"""
        import requests

        mock_response = MagicMock()
        mock_response.status_code = 401
        mock_response.raise_for_status.side_effect = requests.exceptions.HTTPError()
        mock_post.return_value = mock_response

        with pytest.raises(ValueError) as exc_info:
            get_databricks_oauth_token("hostname.com", "client_id", "client_secret")
        self.assertIn("401", str(exc_info.value))

    @patch("ingestr.src.destinations.requests.post")
    def test_missing_access_token_in_response_raises_error(self, mock_post):
        """Test that missing access_token in response raises ValueError"""
        mock_response = MagicMock()
        mock_response.status_code = 200
        mock_response.json.return_value = {"token_type": "Bearer"}  # No access_token
        mock_response.raise_for_status = MagicMock()
        mock_post.return_value = mock_response

        with pytest.raises(ValueError) as exc_info:
            get_databricks_oauth_token("hostname.com", "client_id", "client_secret")
        self.assertIn("access_token", str(exc_info.value))

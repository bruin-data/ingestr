import base64
import json
import os
import tempfile
import unittest

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

    def test_bq_destination_requires_credentials_or_adc(self):
        # When no credentials are provided and use_adc is not set, should raise error
        with pytest.raises(
            ValueError, match="credentials_path or credentials_base64 is required"
        ):
            uri = "bigquery://my-project"
            self.destination.dlt_dest(uri)

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

    def test_bq_destination_raises_filenotfounderror_for_missing_credentials_file(self):
        # Test that FileNotFoundError is raised when credentials_path points to non-existent file
        uri = "bigquery://my-project?credentials_path=/nonexistent/path/to/credentials.json"
        with pytest.raises(FileNotFoundError):
            self.destination.dlt_dest(uri)

    def test_bq_destination_raises_jsondecodeerror_for_invalid_json(self):
        # Create a temporary file with invalid JSON
        with tempfile.NamedTemporaryFile(mode="w", suffix=".json", delete=False) as f:
            f.write("invalid json content {")
            temp_path = f.name

        try:
            uri = f"bigquery://my-project?credentials_path={temp_path}"
            with pytest.raises(json.JSONDecodeError):
                self.destination.dlt_dest(uri)
        finally:
            os.unlink(temp_path)

    def test_bq_destination_raises_error_for_invalid_base64_credentials(self):
        # Test that ValueError is raised when base64 credentials are invalid
        uri = "bigquery://my-project?credentials_base64=invalid_base64!!"
        with pytest.raises((ValueError, UnicodeDecodeError)):
            self.destination.dlt_dest(uri)

    def test_bq_destination_raises_error_for_invalid_json_in_base64_credentials(self):
        # Test that JSONDecodeError is raised when base64 decodes but isn't valid JSON
        invalid_json = base64.b64encode(b"not valid json").decode("utf-8")
        uri = f"bigquery://my-project?credentials_base64={invalid_json}"
        with pytest.raises(json.JSONDecodeError):
            self.destination.dlt_dest(uri)


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


class DatabricksDestinationTest(unittest.TestCase, GenericSqlDestinationFixture):
    destination = DatabricksDestination()
    expected_class = dlt.destinations.databricks

    def test_credentials_are_passed_correctly(self):
        uri = "databricks://token:password@hostname?http_path=/path/123&catalog=workspace&schema=dest"
        result = self.destination.dlt_dest(uri)

        self.assertTrue(isinstance(result, self.expected_class))
        # Override the generic test - expect parsed credentials, not raw URI
        creds = result.config_params["credentials"]
        self.assertEqual(creds["access_token"], "password")
        self.assertEqual(creds["server_hostname"], "hostname")
        self.assertEqual(creds["http_path"], "/path/123")
        self.assertEqual(creds["catalog"], "workspace")
        self.assertEqual(creds["schema"], "dest")

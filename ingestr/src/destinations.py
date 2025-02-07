import base64
import csv
import json
import os
import shutil
import tempfile
from urllib.parse import parse_qs, quote, urlparse

import dlt
from dlt.common.configuration.specs import AwsCredentials
from dlt.destinations.impl.clickhouse.configuration import (
    ClickHouseCredentials,
)

from ingestr.src.loader import load_dlt_file


class GenericSqlDestination:
    def dlt_run_params(self, uri: str, table: str, **kwargs) -> dict:
        table_fields = table.split(".")
        if len(table_fields) != 2:
            raise ValueError("Table name must be in the format <schema>.<table>")

        res = {
            "dataset_name": table_fields[-2],
            "table_name": table_fields[-1],
        }

        return res

    def post_load(self):
        pass


class BigQueryDestination:
    def dlt_dest(self, uri: str, **kwargs):
        source_fields = urlparse(uri)
        source_params = parse_qs(source_fields.query)

        cred_path = source_params.get("credentials_path")
        credentials_base64 = source_params.get("credentials_base64")
        if not cred_path and not credentials_base64:
            raise ValueError(
                "credentials_path or credentials_base64 is required to connect BigQuery"
            )

        location = None
        if source_params.get("location"):
            loc_params = source_params.get("location", [])
            if len(loc_params) > 1:
                raise ValueError("Only one location is allowed")
            location = loc_params[0]

        credentials = {}
        if cred_path:
            with open(cred_path[0], "r") as f:
                credentials = json.load(f)
        elif credentials_base64:
            credentials = json.loads(
                base64.b64decode(credentials_base64[0]).decode("utf-8")
            )

        project_id = None
        if source_fields.hostname:
            project_id = source_fields.hostname

        return dlt.destinations.bigquery(
            credentials=credentials,  # type: ignore
            location=location,
            project_id=project_id,
            **kwargs,
        )

    def dlt_run_params(self, uri: str, table: str, **kwargs) -> dict:
        table_fields = table.split(".")
        if len(table_fields) != 2 and len(table_fields) != 3:
            raise ValueError(
                "Table name must be in the format <dataset>.<table> or <project>.<dataset>.<table>"
            )

        res = {
            "dataset_name": table_fields[-2],
            "table_name": table_fields[-1],
        }

        return res

    def post_load(self):
        pass


class PostgresDestination(GenericSqlDestination):
    def dlt_dest(self, uri: str, **kwargs):
        return dlt.destinations.postgres(credentials=uri, **kwargs)


class SnowflakeDestination(GenericSqlDestination):
    def dlt_dest(self, uri: str, **kwargs):
        return dlt.destinations.snowflake(credentials=uri, **kwargs)


class RedshiftDestination(GenericSqlDestination):
    def dlt_dest(self, uri: str, **kwargs):
        return dlt.destinations.redshift(
            credentials=uri.replace("redshift://", "postgresql://"), **kwargs
        )


class DuckDBDestination(GenericSqlDestination):
    def dlt_dest(self, uri: str, **kwargs):
        return dlt.destinations.duckdb(uri, **kwargs)


class MsSQLDestination(GenericSqlDestination):
    def dlt_dest(self, uri: str, **kwargs):
        return dlt.destinations.mssql(credentials=uri, **kwargs)


class DatabricksDestination(GenericSqlDestination):
    def dlt_dest(self, uri: str, **kwargs):
        return dlt.destinations.databricks(credentials=uri, **kwargs)


class SynapseDestination(GenericSqlDestination):
    def dlt_dest(self, uri: str, **kwargs):
        return dlt.destinations.synapse(credentials=uri, **kwargs)


class CustomCsvDestination(dlt.destinations.filesystem):
    pass


class CsvDestination(GenericSqlDestination):
    temp_path: str
    actual_path: str
    uri: str
    dataset_name: str
    table_name: str

    def dlt_run_params(self, uri: str, table: str, **kwargs) -> dict:
        table_fields = table.split(".")
        if len(table_fields) != 2:
            raise ValueError("Table name must be in the format <schema>.<table>")

        res = {
            "dataset_name": table_fields[-2],
            "table_name": table_fields[-1],
        }

        self.dataset_name = res["dataset_name"]
        self.table_name = res["table_name"]
        self.uri = uri

        return res

    def dlt_dest(self, uri: str, **kwargs):
        if uri.startswith("csv://"):
            uri = uri.replace("csv://", "file://")

        temp_path = tempfile.mkdtemp()
        self.actual_path = uri
        self.temp_path = temp_path
        return CustomCsvDestination(bucket_url=f"file://{temp_path}", **kwargs)

    # I dislike this implementation quite a bit since it ties the implementation to some internal details on how dlt works
    # I would prefer a custom destination that allows me to do this easily but dlt seems to have a lot of internal details that are not documented
    # I tried to make it work with a nicer destination implementation but I couldn't, so I decided to go with this hack to experiment
    # if anyone has a better idea on how to do this, I am open to contributions or suggestions
    def post_load(self):
        def find_first_file(path):
            for entry in os.listdir(path):
                full_path = os.path.join(path, entry)
                if os.path.isfile(full_path):
                    return full_path

            return None

        def filter_keys(dictionary):
            return {
                key: value
                for key, value in dictionary.items()
                if not key.startswith("_dlt_")
            }

        first_file_path = find_first_file(
            f"{self.temp_path}/{self.dataset_name}/{self.table_name}"
        )

        output_path = self.uri.split("://")[1]
        if output_path.count("/") > 1:
            os.makedirs(os.path.dirname(output_path), exist_ok=True)

        with open(output_path, "w", newline="") as csv_file:
            csv_writer = None
            for row in load_dlt_file(first_file_path):
                row = filter_keys(row)
                if csv_writer is None:
                    csv_writer = csv.DictWriter(csv_file, fieldnames=row.keys())
                    csv_writer.writeheader()

                csv_writer.writerow(row)
        shutil.rmtree(self.temp_path)


class AthenaDestination:
    def dlt_dest(self, uri: str, **kwargs):
        encoded_uri = quote(uri, safe=":/?&=")
        source_fields = urlparse(encoded_uri)
        source_params = parse_qs(source_fields.query)

        bucket = source_params.get("bucket", [None])[0]
        if not bucket:
            raise ValueError("A bucket is required to connect to Athena.")

        if not bucket.startswith("s3://"):
            bucket = f"s3://{bucket}"

        query_result_path = source_params.get("query_results_path", [None])[0]
        if query_result_path:
            if not query_result_path.startswith("s3://"):
                query_result_path = f"s3://{query_result_path}"
        else:
            query_result_path = bucket

        access_key_id = source_params.get("access_key_id", [None])[0]
        if not access_key_id:
            raise ValueError("The AWS access_key_id is required to connect to Athena.")

        secret_access_key = source_params.get("secret_access_key", [None])[0]
        if not secret_access_key:
            raise ValueError("The AWS secret_access_key is required to connect Athena")

        work_group = source_params.get("workgroup", [None])[0]

        region_name = source_params.get("region_name", [None])[0]
        if not region_name:
            raise ValueError("The region_name is required to connect to Athena.")

        os.environ["DESTINATION__BUCKET_URL"] = bucket
        os.environ["DESTINATION__CREDENTIALS__AWS_ACCESS_KEY_ID"] = access_key_id
        os.environ["DESTINATION__CREDENTIALS__AWS_SECRET_ACCESS_KEY"] = (
            secret_access_key
        )

        credentials = AwsCredentials(
            aws_access_key_id=access_key_id,
            aws_secret_access_key=secret_access_key,
            region_name=region_name,
        )
        return dlt.destinations.athena(
            query_result_bucket=query_result_path,
            athena_work_group=work_group,
            credentials=credentials,
            destination_name=bucket,
        )

    def dlt_run_params(self, uri: str, table: str, **kwargs) -> dict:
        table_fields = table.split(".")
        if len(table_fields) != 2:
            raise ValueError("Table name must be in the format <schema>.<table>")
        return {
            "table_format": "iceberg",
            "dataset_name": table_fields[-2],
            "table_name": table_fields[-1],
        }

    def post_load(self):
        pass


class ClickhouseDestination:
    def dlt_dest(self, uri: str, **kwargs):
        parsed_uri = urlparse(uri)

        if "dest_table" in kwargs:
            table = kwargs["dest_table"]
            database = table.split(".")[0]
        else:
            database = parsed_uri.path.lstrip("/")

        username = parsed_uri.username
        if not username:
            raise ValueError(
                "A username is required to connect to the ClickHouse database."
            )

        password = parsed_uri.password
        if not password:
            raise ValueError(
                "A password is required to authenticate with the ClickHouse database."
            )

        host = parsed_uri.hostname
        if not host:
            raise ValueError(
                "The hostname or IP address of the ClickHouse server is required to establish a connection."
            )

        port = parsed_uri.port
        if not port:
            raise ValueError(
                "The TCP port of the ClickHouse server is required to establish a connection."
            )

        query_params = parse_qs(parsed_uri.query)
        secure = int(query_params["secure"][0]) if "secure" in query_params else 1

        http_port = (
            int(query_params["http_port"][0])
            if "http_port" in query_params
            else 8443
            if secure == 1
            else 8123
        )

        if secure not in (0, 1):
            raise ValueError(
                "Invalid value for secure. Set to `1` for a secure HTTPS connection or `0` for a non-secure HTTP connection."
            )

        credentials = ClickHouseCredentials(
            {
                "host": host,
                "port": port,
                "username": username,
                "password": password,
                "database": database,
                "http_port": http_port,
                "secure": secure,
            }
        )
        return dlt.destinations.clickhouse(credentials=credentials)

    def dlt_run_params(self, uri: str, table: str, **kwargs) -> dict:
        table_fields = table.split(".")
        if len(table_fields) != 2:
            raise ValueError("Table name must be in the format <schema>.<table>")
        return {
            "table_name": table_fields[-1],
        }

    def post_load(self):
        pass

import base64
import csv
import gzip
import json
import os
import shutil
import tempfile
from urllib.parse import parse_qs, quote, urlparse

import dlt
from dlt.common.configuration.specs import AwsCredentials


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

        return dlt.destinations.bigquery(
            credentials=credentials,  # type: ignore
            location=location,
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

        with gzip.open(first_file_path, "rt", encoding="utf-8") as jsonl_file:  # type: ignore
            with open(output_path, "w", newline="") as csv_file:
                csv_writer = None
                for line in jsonl_file:
                    json_obj = filter_keys(json.loads(line))
                    if csv_writer is None:
                        csv_writer = csv.DictWriter(
                            csv_file, fieldnames=json_obj.keys()
                        )
                        csv_writer.writeheader()

                    csv_writer.writerow(json_obj)

        shutil.rmtree(self.temp_path)


class AthenaDestination(GenericSqlDestination):
    def dlt_dest(self, uri: str, **kwargs):
        encoded_uri = quote(uri, safe=":/?&=")
        source_fields = urlparse(encoded_uri)
        source_params = parse_qs(source_fields.query)

        bucket_url = source_params.get("bucket_url")
        if not bucket_url:
            raise ValueError("bucket_url is required to connect Athena")

        query_result_url = source_params.get("query_result_url")
        if not query_result_url:
            raise ValueError("query_result_url is required to connect Athena")

        aws_access_key_id = source_params.get("aws_access_key_id")
        if not aws_access_key_id:
            raise ValueError("aws_access_key_id is required to connect Athena")

        aws_secret_access_key = source_params.get("aws_secret_access_key")
        if not aws_secret_access_key:
            raise ValueError("aws_secret_access_key are required to connect Athena")

        athena_work_group = source_params.get("athena_work_group")
        if not athena_work_group:
            raise ValueError("athena_work_group is required to connect Athena")

        region_name = source_params.get("region_name")
        if not region_name:
            raise ValueError("region_name is required to connect Athena")

        os.environ["BUCKET_URL"] = bucket_url[0]
        os.environ["AWS_ACCESS_KEY_ID"] = aws_access_key_id[0]
        os.environ["AWS_SECRET_ACCESS_KEY"] = aws_secret_access_key[0]

        credentials = AwsCredentials(
            aws_access_key_id=aws_access_key_id[0],
            aws_secret_access_key=aws_secret_access_key[0],
            region_name=region_name[0],
        )

        return dlt.destinations.athena(
            query_result_bucket=query_result_url[0],
            athena_work_group=athena_work_group[0],
            credentials=credentials,
            destination_name=bucket_url[0],
        )

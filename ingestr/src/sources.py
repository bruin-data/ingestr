import base64
import csv
import json
from typing import Callable
from urllib.parse import parse_qs, urlparse

import dlt

from ingestr.src.google_sheets import google_spreadsheet
from ingestr.src.mongodb import mongodb_collection
from ingestr.src.notion import notion_databases
from ingestr.src.sql_database import sql_table


class SqlSource:
    table_builder: Callable

    def __init__(self, table_builder=sql_table) -> None:
        self.table_builder = table_builder

    def dlt_source(self, uri: str, table: str, **kwargs):
        table_fields = table.split(".")
        if len(table_fields) != 2:
            raise ValueError("Table name must be in the format schema.table")

        incremental = None
        if kwargs.get("incremental_key"):
            start_value = kwargs.get("interval_start")
            end_value = kwargs.get("interval_end")

            incremental = dlt.sources.incremental(
                kwargs.get("incremental_key", ""),
                # primary_key=(),
                initial_value=start_value,
                end_value=end_value,
            )

        if uri.startswith("mysql://"):
            uri = uri.replace("mysql://", "mysql+pymysql://")

        table_instance = self.table_builder(
            credentials=uri,
            schema=table_fields[-2],
            table=table_fields[-1],
            incremental=incremental,
            merge_key=kwargs.get("merge_key"),
            backend=kwargs.get("sql_backend", "sqlalchemy"),
        )

        return table_instance


class MongoDbSource:
    table_builder: Callable

    def __init__(self, table_builder=mongodb_collection) -> None:
        self.table_builder = table_builder

    def dlt_source(self, uri: str, table: str, **kwargs):
        table_fields = table.split(".")
        if len(table_fields) != 2:
            raise ValueError("Table name must be in the format schema.table")

        incremental = None
        if kwargs.get("incremental_key"):
            start_value = kwargs.get("interval_start")
            end_value = kwargs.get("interval_end")

            incremental = dlt.sources.incremental(
                kwargs.get("incremental_key", ""),
                initial_value=start_value,
                end_value=end_value,
            )

        table_instance = self.table_builder(
            connection_url=uri,
            database=table_fields[-2],
            collection=table_fields[-1],
            parallel=True,
            incremental=incremental,
        )

        return table_instance


class LocalCsvSource:
    def dlt_source(self, uri: str, table: str, **kwargs):
        def csv_file():
            file_path = uri.split("://")[1]
            myFile = open(file_path, "r")
            reader = csv.DictReader(myFile)
            print("running resource")

            page_size = 1000
            page = []
            current_items = 0
            for dictionary in reader:
                if current_items < page_size:
                    page.append(dictionary)
                    current_items += 1
                else:
                    yield page
                    page = []
                    current_items = 0

            if page:
                yield page

        return dlt.resource(
            csv_file,
            merge_key=kwargs.get("merge_key"),  # type: ignore
        )


class NotionSource:
    table_builder: Callable

    def __init__(self, table_builder=notion_databases) -> None:
        self.table_builder = table_builder

    def dlt_source(self, uri: str, table: str, **kwargs):
        if kwargs.get("incremental_key"):
            raise ValueError("Incremental loads are not supported for Notion")

        source_fields = urlparse(uri)
        source_params = parse_qs(source_fields.query)
        api_key = source_params.get("api_key")
        if not api_key:
            raise ValueError("api_key in the URI is required to connect to Notion")

        return self.table_builder(
            database_ids=[{"id": table}],
            api_key=api_key[0],
        )


class GoogleSheetsSource:
    table_builder: Callable

    def __init__(self, table_builder=google_spreadsheet) -> None:
        self.table_builder = table_builder

    def dlt_source(self, uri: str, table: str, **kwargs):
        if kwargs.get("incremental_key"):
            raise ValueError("Incremental loads are not supported for Google Sheets")

        source_fields = urlparse(uri)
        source_params = parse_qs(source_fields.query)

        cred_path = source_params.get("credentials_path")
        credentials_base64 = source_params.get("credentials_base64")
        if not cred_path and not credentials_base64:
            raise ValueError(
                "credentials_path or credentials_base64 is required in the URI to get data from Google Sheets"
            )

        credentials = {}
        if cred_path:
            with open(cred_path[0], "r") as f:
                credentials = json.load(f)
        elif credentials_base64:
            credentials = json.loads(
                base64.b64decode(credentials_base64[0]).decode("utf-8")
            )

        table_fields = table.split(".")
        if len(table_fields) != 2:
            raise ValueError(
                "Table name must be in the format <spreadsheet_id>.<sheet_name>"
            )

        return self.table_builder(
            credentials=credentials,
            spreadsheet_url_or_id=table_fields[0],
            range_names=[table_fields[1]],
            get_named_ranges=False,
        )

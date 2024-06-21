import base64
import csv
import json
from typing import Any, Callable, Optional
from urllib.parse import parse_qs, urlparse

import dlt

from ingestr.src.google_sheets import google_spreadsheet
from ingestr.src.gorgias import gorgias_source
from ingestr.src.mongodb import mongodb_collection
from ingestr.src.notion import notion_databases
from ingestr.src.shopify import shopify_source
from ingestr.src.sql_database import sql_table


class SqlSource:
    table_builder: Callable

    def __init__(self, table_builder=sql_table) -> None:
        self.table_builder = table_builder

    def handles_incrementality(self) -> bool:
        return False

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

    def handles_incrementality(self) -> bool:
        return False

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
    def handles_incrementality(self) -> bool:
        return False

    def dlt_source(self, uri: str, table: str, **kwargs):
        def csv_file(
            incremental: Optional[dlt.sources.incremental[Any]] = None,
        ):
            file_path = uri.split("://")[1]
            myFile = open(file_path, "r")
            reader = csv.DictReader(myFile)
            if not reader.fieldnames:
                raise RuntimeError(
                    "failed to extract headers from the CSV, are you sure the given file contains a header row?"
                )

            incremental_key = kwargs.get("incremental_key")
            if incremental_key and incremental_key not in reader.fieldnames:
                raise ValueError(
                    f"incremental_key '{incremental_key}' not found in the CSV file"
                )

            page_size = 1000
            page = []
            current_items = 0
            for dictionary in reader:
                if current_items < page_size:
                    if incremental_key and incremental and incremental.start_value:
                        inc_value = dictionary.get(incremental_key)
                        if inc_value is None:
                            raise ValueError(
                                f"incremental_key '{incremental_key}' not found in the CSV file"
                            )

                        print("BURAYA GELLDIII")
                        if inc_value < incremental.start_value:
                            continue

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
        )(
            incremental=dlt.sources.incremental(
                kwargs.get("incremental_key", ""),
                initial_value=kwargs.get("interval_start"),
                end_value=kwargs.get("interval_end"),
            )
        )


class NotionSource:
    table_builder: Callable

    def __init__(self, table_builder=notion_databases) -> None:
        self.table_builder = table_builder

    def handles_incrementality(self) -> bool:
        return True

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


class ShopifySource:
    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        if kwargs.get("incremental_key"):
            raise ValueError(
                "Shopify takes care of incrementality on its own, you should not provide incremental_key"
            )

        # shopify://shop_url?api_key=private_app_password

        source_fields = urlparse(uri)
        source_params = parse_qs(source_fields.query)
        api_key = source_params.get("api_key")
        if not api_key:
            raise ValueError("api_key in the URI is required to connect to Shopify")

        date_args = {}
        if kwargs.get("interval_start"):
            date_args["start_date"] = kwargs.get("interval_start")

        if kwargs.get("interval_end"):
            date_args["end_date"] = kwargs.get("interval_end")

        resource = None
        if table in ["products", "orders", "customers"]:
            resource = table
        else:
            raise ValueError(
                f"Table name '{table}' is not supported for Shopify source yet, if you are interested in it please create a GitHub issue at https://github.com/bruin-data/ingestr"
            )

        return shopify_source(
            private_app_password=api_key[0],
            shop_url=f"https://{source_fields.netloc}",
            **date_args,
        ).with_resources(resource)


class GorgiasSource:
    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        if kwargs.get("incremental_key"):
            raise ValueError(
                "Gorgias takes care of incrementality on its own, you should not provide incremental_key"
            )

        # gorgias://domain?api_key=<api_key>&email=<email>

        source_fields = urlparse(uri)
        source_params = parse_qs(source_fields.query)
        api_key = source_params.get("api_key")
        if not api_key:
            raise ValueError("api_key in the URI is required to connect to Gorgias")

        email = source_params.get("email")
        if not email:
            raise ValueError("email in the URI is required to connect to Gorgias")

        resource = None
        if table in ["customers", "tickets", "ticket_messages", "satisfaction_surveys"]:
            resource = table
        else:
            raise ValueError(
                f"Resource '{table}' is not supported for Gorgias source yet, if you are interested in it please create a GitHub issue at https://github.com/bruin-data/ingestr"
            )

        date_args = {}
        if kwargs.get("interval_start"):
            date_args["start_date"] = kwargs.get("interval_start")

        if kwargs.get("interval_end"):
            date_args["end_date"] = kwargs.get("interval_end")

        return gorgias_source(
            domain=source_fields.netloc,
            email=email[0],
            api_key=api_key[0],
            **date_args,
        ).with_resources(resource)


class GoogleSheetsSource:
    table_builder: Callable

    def __init__(self, table_builder=google_spreadsheet) -> None:
        self.table_builder = table_builder

    def handles_incrementality(self) -> bool:
        return False

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

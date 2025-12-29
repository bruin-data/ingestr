import base64
import csv
import json
import os
import re
import sys
import tempfile
from datetime import date, datetime, timedelta, timezone
from typing import (
    Any,
    Callable,
    Dict,
    Iterator,
    List,
    Literal,
    Optional,
    TypeAlias,
    Union,
)
from urllib.parse import ParseResult, parse_qs, urlencode, urlparse

import fsspec  # type: ignore
import pendulum
from dlt.common.time import ensure_pendulum_datetime
from dlt.extract import Incremental
from dlt.extract.exceptions import ResourcesNotFoundError
from dlt.sources import incremental as dlt_incremental
from dlt.sources.credentials import (
    ConnectionStringCredentials,
)

from ingestr.src import blob
from ingestr.src.errors import (
    InvalidBlobTableError,
    MissingValueError,
    UnsupportedResourceError,
)
from ingestr.src.table_definition import TableDefinition, table_string_to_dataclass


class SqlSource:
    table_builder: Callable

    def __init__(self, table_builder=None) -> None:
        if table_builder is None:
            from dlt.sources.sql_database import sql_table

            table_builder = sql_table

        self.table_builder = table_builder

    def handles_incrementality(self) -> bool:
        return False

    def dlt_source(self, uri: str, table: str, **kwargs):
        table_fields = TableDefinition(dataset="custom", table="custom")
        if not table.startswith("query:"):
            if uri.startswith("spanner://"):
                table_fields = TableDefinition(dataset="", table=table)
            else:
                table_fields = table_string_to_dataclass(table)

        incremental = None
        if kwargs.get("incremental_key"):
            start_value = kwargs.get("interval_start")
            end_value = kwargs.get("interval_end")
            incremental = dlt_incremental(
                kwargs.get("incremental_key", ""),
                initial_value=start_value,
                end_value=end_value,
                range_end="closed",
                range_start="closed",
            )

        engine_adapter_callback = None

        if uri.startswith("md://") or uri.startswith("motherduck://"):
            parsed_uri = urlparse(uri)
            query_params = parse_qs(parsed_uri.query)
            # Convert md:// URI to duckdb:///md: format
            if parsed_uri.path:
                db_path = parsed_uri.path
            else:
                db_path = ""

            token = query_params.get("token", [""])[0]
            if not token:
                raise ValueError("Token is required for MotherDuck connection")
            uri = f"duckdb:///md:{db_path}?motherduck_token={token}"

        if uri.startswith("mysql://"):
            uri = uri.replace("mysql://", "mysql+pymysql://")

        # Monkey patch cx_Oracle to use oracledb (thin mode, no client libraries required)
        if uri.startswith("oracle+") or uri.startswith("oracle://"):
            try:
                import oracledb  # type: ignore[import-not-found]

                # SQLAlchemy's cx_oracle dialect checks for version >= 5.2
                # oracledb has a different versioning scheme, so we need to patch it
                oracledb.version = "8.3.0"  # type: ignore[assignment]
                sys.modules["cx_Oracle"] = oracledb  # type: ignore[assignment]
            except ImportError:
                # oracledb not installed, will fail later with a clear error
                pass

        # Process Snowflake private key authentication
        if uri.startswith("snowflake://"):
            parsed_uri = urlparse(uri)
            query_params = parse_qs(parsed_uri.query)

            if "private_key" in query_params:
                from dlt.common.libs.cryptography import decode_private_key

                private_key = query_params["private_key"][0]
                passphrase = query_params.get("private_key_passphrase", [None])[0]
                decoded_key = decode_private_key(private_key, passphrase)

                query_params["private_key"] = [base64.b64encode(decoded_key).decode()]
                if "private_key_passphrase" in query_params:
                    del query_params["private_key_passphrase"]

                # Rebuild URI
                uri = parsed_uri._replace(
                    query=urlencode(query_params, doseq=True)
                ).geturl()

        # clickhouse://<username>:<password>@<host>:<port>?secure=<secure>
        if uri.startswith("clickhouse://"):
            parsed_uri = urlparse(uri)

            query_params = parse_qs(parsed_uri.query)

            if "http_port" in query_params:
                del query_params["http_port"]

            if "secure" not in query_params:
                query_params["secure"] = ["1"]

            uri = parsed_uri._replace(
                scheme="clickhouse+native",
                query=urlencode(query_params, doseq=True),
            ).geturl()

        if uri.startswith("db2://"):
            uri = uri.replace("db2://", "db2+ibm_db://")

        if uri.startswith("spanner://"):
            parsed_uri = urlparse(uri)
            query_params = parse_qs(parsed_uri.query)

            project_id_param = query_params.get("project_id")
            instance_id_param = query_params.get("instance_id")
            database_param = query_params.get("database")

            cred_path = query_params.get("credentials_path")
            cred_base64 = query_params.get("credentials_base64")

            if not project_id_param or not instance_id_param or not database_param:
                raise ValueError(
                    "project_id, instance_id and database are required in the URI to get data from Google Spanner"
                )

            project_id = project_id_param[0]
            instance_id = instance_id_param[0]
            database = database_param[0]

            if not cred_path and not cred_base64:
                raise ValueError(
                    "credentials_path or credentials_base64 is required in the URI to get data from Google Sheets"
                )
            if cred_path:
                os.environ["GOOGLE_APPLICATION_CREDENTIALS"] = cred_path[0]
            elif cred_base64:
                credentials = json.loads(
                    base64.b64decode(cred_base64[0]).decode("utf-8")
                )
                temp = tempfile.NamedTemporaryFile(
                    mode="w", delete=False, suffix=".json"
                )
                json.dump(credentials, temp)
                temp.close()
                os.environ["GOOGLE_APPLICATION_CREDENTIALS"] = temp.name

            uri = f"spanner+spanner:///projects/{project_id}/instances/{instance_id}/databases/{database}"

            def eng_callback(engine):
                return engine.execution_options(read_only=True)

            engine_adapter_callback = eng_callback
        from dlt.common.libs.sql_alchemy import (
            Engine,
            MetaData,
        )
        from dlt.sources.sql_database.schema_types import (
            ReflectionLevel,
            SelectAny,
            Table,
            TTypeAdapter,
        )
        from sqlalchemy import Column
        from sqlalchemy import types as sa

        from ingestr.src.filters import table_adapter_exclude_columns
        from ingestr.src.sql_database.callbacks import (
            chained_query_adapter_callback,
            custom_query_variable_subsitution,
            limit_callback,
            type_adapter_callback,
        )

        query_adapters = []
        if kwargs.get("sql_limit"):
            query_adapters.append(
                limit_callback(kwargs.get("sql_limit"), kwargs.get("incremental_key"))
            )

        defer_table_reflect = False
        sql_backend = kwargs.get("sql_backend", "sqlalchemy")
        if table.startswith("query:"):
            if kwargs.get("sql_limit"):
                raise ValueError(
                    "sql_limit is not supported for custom queries, please apply the limit in the query instead"
                )

            sql_backend = "sqlalchemy"
            defer_table_reflect = True
            query_value = table.split(":", 1)[1]

            TableBackend: TypeAlias = Literal[
                "sqlalchemy", "pyarrow", "pandas", "connectorx"
            ]
            TQueryAdapter: TypeAlias = Callable[[SelectAny, Table], SelectAny]
            import dlt
            from dlt.common.typing import TDataItem

            # this is a very hacky version of the table_rows function. it is built this way to go around the dlt's table loader.
            # I didn't want to write a full fledged sqlalchemy source for now, and wanted to benefit from the existing stuff to begin with.
            # this is by no means a production ready solution, but it works for now.
            # the core idea behind this implementation is to create a mock table instance with the columns that are absolutely necessary for the incremental load to work.
            # the table loader will then use the query adapter callback to apply the actual query and load the rows.
            def table_rows(
                engine: Engine,
                table: Union[Table, str],
                metadata: MetaData,
                chunk_size: int,
                backend: TableBackend,
                incremental: Optional[Incremental[Any]] = None,
                table_adapter_callback: Callable[[Table], None] = None,  # type: ignore
                reflection_level: ReflectionLevel = "minimal",
                backend_kwargs: Dict[str, Any] = None,  # type: ignore
                type_adapter_callback: Optional[TTypeAdapter] = None,
                included_columns: Optional[List[str]] = None,
                excluded_columns: Optional[
                    List[str]
                ] = None,  # Added for dlt 1.16.0 compatibility
                query_adapter_callback: Optional[TQueryAdapter] = None,
                resolve_foreign_keys: bool = False,
            ) -> Iterator[TDataItem]:
                hints = {  # type: ignore
                    "columns": [],
                }
                cols = []  # type: ignore

                if incremental:
                    switchDict = {
                        int: sa.INTEGER,
                        datetime: sa.TIMESTAMP,
                        date: sa.DATE,
                        pendulum.Date: sa.DATE,
                        pendulum.DateTime: sa.TIMESTAMP,
                    }

                    if incremental.last_value is not None:
                        cols.append(
                            Column(
                                incremental.cursor_path,
                                switchDict[type(incremental.last_value)],  # type: ignore
                            )
                        )
                    else:
                        cols.append(Column(incremental.cursor_path, sa.TIMESTAMP))  # type: ignore

                table = Table(
                    "query_result",
                    metadata,
                    *cols,
                )

                from dlt.sources.sql_database.helpers import TableLoader

                loader = TableLoader(
                    engine,
                    backend,
                    table,
                    hints["columns"],  # type: ignore
                    incremental=incremental,
                    chunk_size=chunk_size,
                    query_adapter_callback=query_adapter_callback,
                )
                try:
                    yield from loader.load_rows(backend_kwargs)
                finally:
                    if getattr(engine, "may_dispose_after_use", False):
                        engine.dispose()

            dlt.sources.sql_database.table_rows = table_rows  # type: ignore

            # override the query adapters, the only one we want is the one here in the case of custom queries
            query_adapters = [custom_query_variable_subsitution(query_value, kwargs)]

        credentials = ConnectionStringCredentials(uri)
        if uri.startswith("mssql://"):
            parsed_uri = urlparse(uri)
            params = parse_qs(parsed_uri.query)
            params = {k.lower(): v for k, v in params.items()}
            if params.get("authentication") == ["ActiveDirectoryAccessToken"]:
                import pyodbc  # type: ignore
                from sqlalchemy import create_engine

                from ingestr.src.destinations import (
                    MSSQL_COPT_SS_ACCESS_TOKEN,
                    handle_datetimeoffset,
                    serialize_azure_token,
                )

                cfg = {
                    "DRIVER": params.get("driver", ["ODBC Driver 18 for SQL Server"])[
                        0
                    ],
                    "SERVER": f"{parsed_uri.hostname},{parsed_uri.port or 1433}",
                    "DATABASE": parsed_uri.path.lstrip("/"),
                }
                for k, v in params.items():
                    if k.lower() not in ["driver", "authentication", "connect_timeout"]:
                        cfg[k.upper()] = v[0]

                token = serialize_azure_token(parsed_uri.password)
                dsn = ";".join([f"{k}={v}" for k, v in cfg.items()])

                def creator():
                    connection = pyodbc.connect(
                        dsn,
                        autocommit=True,
                        timeout=kwargs.get("connect_timeout", 30),
                        attrs_before={
                            MSSQL_COPT_SS_ACCESS_TOKEN: token,
                        },
                    )
                    connection.add_output_converter(-155, handle_datetimeoffset)
                    return connection

                credentials = create_engine(
                    "mssql+pyodbc://",
                    creator=creator,
                )

        builder_res = self.table_builder(
            credentials=credentials,
            schema=table_fields.dataset,
            table=table_fields.table,
            incremental=incremental,
            backend=sql_backend,
            chunk_size=kwargs.get("page_size", None),
            reflection_level=kwargs.get("sql_reflection_level", None),
            query_adapter_callback=chained_query_adapter_callback(query_adapters),
            type_adapter_callback=type_adapter_callback,
            table_adapter_callback=table_adapter_exclude_columns(
                kwargs.get("sql_exclude_columns", [])
            ),
            defer_table_reflect=defer_table_reflect,
            engine_adapter_callback=engine_adapter_callback,
        )

        return builder_res


class ArrowMemoryMappedSource:
    table_builder: Callable

    def __init__(self, table_builder=None) -> None:
        if table_builder is None:
            from ingestr.src.arrow import memory_mapped_arrow

            table_builder = memory_mapped_arrow

        self.table_builder = table_builder

    def handles_incrementality(self) -> bool:
        return False

    def dlt_source(self, uri: str, table: str, **kwargs):
        incremental = None
        if kwargs.get("incremental_key"):
            start_value = kwargs.get("interval_start")
            end_value = kwargs.get("interval_end")

            incremental = dlt_incremental(
                kwargs.get("incremental_key", ""),
                initial_value=start_value,
                end_value=end_value,
                range_end="closed",
                range_start="closed",
            )

        file_path = uri.split("://")[1]
        if not os.path.exists(file_path):
            raise ValueError(f"File at path {file_path} does not exist")

        if os.path.isdir(file_path):
            raise ValueError(
                f"Path {file_path} is a directory, it should be an Arrow memory mapped file"
            )

        primary_key = kwargs.get("primary_key")
        merge_key = kwargs.get("merge_key")

        table_instance = self.table_builder(
            path=file_path,
            incremental=incremental,
            merge_key=merge_key,
            primary_key=primary_key,
        )

        return table_instance


class MongoDbSource:
    table_builder: Callable

    def __init__(self, table_builder=None) -> None:
        if table_builder is None:
            from ingestr.src.mongodb import mongodb_collection

            table_builder = mongodb_collection

        self.table_builder = table_builder

    def handles_incrementality(self) -> bool:
        return False

    def dlt_source(self, uri: str, table: str, **kwargs):
        # Check if this is a custom query format (collection:query)
        if ":" in table:
            collection_name, query_json = table.split(":", 1)

            # Parse the query using MongoDB's extended JSON parser
            # First, convert MongoDB shell syntax to Extended JSON format
            from bson import json_util

            from ingestr.src.mongodb.helpers import convert_mongo_shell_to_extended_json

            # Convert MongoDB shell constructs to Extended JSON v2 format
            converted_query = convert_mongo_shell_to_extended_json(query_json)

            try:
                query = json_util.loads(converted_query)
            except Exception as e:
                raise ValueError(f"Invalid MongoDB query format: {e}")

            # Validate that it's a list for aggregation pipeline
            if not isinstance(query, list):
                raise ValueError(
                    "Query must be a JSON array representing a MongoDB aggregation pipeline"
                )

            # Check for incremental load requirements
            incremental = None
            if kwargs.get("incremental_key"):
                start_value = kwargs.get("interval_start")
                end_value = kwargs.get("interval_end")

                # Validate that incremental key is present in the pipeline
                incremental_key = kwargs.get("incremental_key")
                self._validate_incremental_query(query, str(incremental_key))

                incremental = dlt_incremental(
                    str(incremental_key),
                    initial_value=start_value,
                    end_value=end_value,
                )

                # Substitute interval parameters in the query
                query = self._substitute_interval_params(query, kwargs)

            # Parse collection name to get database and collection
            if "." in collection_name:
                # Handle database.collection format
                table_fields = table_string_to_dataclass(collection_name)
                database = table_fields.dataset
                collection = table_fields.table
            else:
                # Single collection name, use default database
                database = None
                collection = collection_name

            table_instance = self.table_builder(
                connection_url=uri,
                database=database,
                collection=collection,
                parallel=False,
                incremental=incremental,
                custom_query=query,
            )
            table_instance.max_table_nesting = 1
            return table_instance
        else:
            # Default behavior for simple collection names
            table_fields = table_string_to_dataclass(table)

            incremental = None
            if kwargs.get("incremental_key"):
                start_value = kwargs.get("interval_start")
                end_value = kwargs.get("interval_end")

                incremental = dlt_incremental(
                    kwargs.get("incremental_key", ""),
                    initial_value=start_value,
                    end_value=end_value,
                )

            table_instance = self.table_builder(
                connection_url=uri,
                database=table_fields.dataset,
                collection=table_fields.table,
                parallel=False,
                incremental=incremental,
            )
            table_instance.max_table_nesting = 1

            return table_instance

    def _validate_incremental_query(self, query: list, incremental_key: str):
        """Validate that incremental key is projected in the aggregation pipeline"""
        # Check if there's a $project stage and if incremental_key is included
        has_project = False
        incremental_key_projected = False

        for stage in query:
            if "$project" in stage:
                has_project = True
                project_stage = stage["$project"]
                if isinstance(project_stage, dict):
                    # Check if incremental_key is explicitly included
                    if incremental_key in project_stage:
                        if project_stage[incremental_key] not in [0, False]:
                            incremental_key_projected = True
                    # If there are only inclusions (1 or True values) and incremental_key is not included
                    elif any(v in [1, True] for v in project_stage.values()):
                        # This is an inclusion projection, incremental_key must be explicitly included
                        incremental_key_projected = False
                    # If there are only exclusions (0 or False values) and incremental_key is not excluded
                    elif all(
                        v in [0, False]
                        for v in project_stage.values()
                        if v in [0, False, 1, True]
                    ):
                        # This is an exclusion projection, incremental_key is included by default
                        if incremental_key not in project_stage:
                            incremental_key_projected = True
                        else:
                            incremental_key_projected = project_stage[
                                incremental_key
                            ] not in [0, False]
                    else:
                        # Mixed or unclear projection, assume incremental_key needs to be explicit
                        incremental_key_projected = False

        # If there's a $project stage but incremental_key is not projected, raise error
        if has_project and not incremental_key_projected:
            raise ValueError(
                f"Incremental key '{incremental_key}' must be included in the projected fields of the aggregation pipeline"
            )

    def _substitute_interval_params(self, query: list, kwargs: dict):
        """Substitute :interval_start and :interval_end placeholders with actual datetime values"""
        from dlt.common.time import ensure_pendulum_datetime

        # Get interval values and convert them to datetime objects
        interval_start = kwargs.get("interval_start")
        interval_end = kwargs.get("interval_end")

        # Convert string dates to datetime objects if needed
        if interval_start is not None:
            if isinstance(interval_start, str):
                pendulum_dt = ensure_pendulum_datetime(interval_start)
                interval_start = (
                    pendulum_dt.to_datetime()
                    if hasattr(pendulum_dt, "to_datetime")
                    else pendulum_dt
                )
            elif hasattr(interval_start, "to_datetime"):
                interval_start = interval_start.to_datetime()

        if interval_end is not None:
            if isinstance(interval_end, str):
                pendulum_dt = ensure_pendulum_datetime(interval_end)
                interval_end = (
                    pendulum_dt.to_datetime()
                    if hasattr(pendulum_dt, "to_datetime")
                    else pendulum_dt
                )
            elif hasattr(interval_end, "to_datetime"):
                interval_end = interval_end.to_datetime()

        # Deep copy the query and replace placeholders with actual datetime objects
        def replace_placeholders(obj):
            if isinstance(obj, dict):
                result = {}
                for key, value in obj.items():
                    if value == ":interval_start" and interval_start is not None:
                        result[key] = interval_start
                    elif value == ":interval_end" and interval_end is not None:
                        result[key] = interval_end
                    else:
                        result[key] = replace_placeholders(value)
                return result
            elif isinstance(obj, list):
                return [replace_placeholders(item) for item in obj]
            else:
                return obj

        return replace_placeholders(query)


class LocalCsvSource:
    def handles_incrementality(self) -> bool:
        return False

    def dlt_source(self, uri: str, table: str, **kwargs):
        def csv_file(
            incremental: Optional[dlt_incremental[Any]] = None,
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
                # Skip rows where all values are None or empty/whitespace
                if all(
                    v is None or (isinstance(v, str) and v.strip() == "")
                    for v in dictionary.values()
                ):
                    continue

                if current_items < page_size:
                    if incremental_key and incremental and incremental.start_value:
                        inc_value = dictionary.get(incremental_key)
                        if inc_value is None:
                            raise ValueError(
                                f"incremental_key '{incremental_key}' not found in the CSV file"
                            )

                        if inc_value < incremental.start_value:
                            continue

                    dictionary = self.remove_empty_columns(dictionary)
                    page.append(dictionary)
                    current_items += 1
                else:
                    yield page
                    page = []
                    current_items = 0

            if page:
                yield page

        from dlt import resource

        return resource(
            csv_file,
            merge_key=kwargs.get("merge_key"),  # type: ignore
        )(
            incremental=dlt_incremental(
                kwargs.get("incremental_key", ""),
                initial_value=kwargs.get("interval_start"),
                end_value=kwargs.get("interval_end"),
                range_end="closed",
                range_start="closed",
            )
        )

    def remove_empty_columns(self, row: Dict[str, str]) -> Dict[str, str]:
        return {k: v for k, v in row.items() if v.strip() != ""}


class NotionSource:
    table_builder: Callable

    def __init__(self, table_builder=None) -> None:
        if table_builder is None:
            from ingestr.src.notion import notion_databases

            table_builder = notion_databases

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
        if table in [
            "products",
            "products_legacy",
            "orders",
            "customers",
            "inventory_items",
            "transactions",
            "balance",
            "events",
            "price_rules",
            "discounts",
            "taxonomy",
        ]:
            resource = table
        else:
            raise ValueError(
                f"Table name '{table}' is not supported for Shopify source yet, if you are interested in it please create a GitHub issue at https://github.com/bruin-data/ingestr"
            )

        from ingestr.src.shopify import shopify_source

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

        from ingestr.src.gorgias import gorgias_source

        return gorgias_source(
            domain=source_fields.netloc,
            email=email[0],
            api_key=api_key[0],
            **date_args,
        ).with_resources(resource)


class GoogleSheetsSource:
    table_builder: Callable

    def __init__(self, table_builder=None) -> None:
        if table_builder is None:
            from ingestr.src.google_sheets import google_spreadsheet

            table_builder = google_spreadsheet

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

        table_fields = table_string_to_dataclass(table)
        return self.table_builder(
            credentials=credentials,
            spreadsheet_url_or_id=table_fields.dataset,
            range_names=[table_fields.table],
            get_named_ranges=False,
        )


class ChessSource:
    def handles_incrementality(self) -> bool:
        return True

    # chess://?players=john,peter
    def dlt_source(self, uri: str, table: str, **kwargs):
        if kwargs.get("incremental_key"):
            raise ValueError(
                "Chess takes care of incrementality on its own, you should not provide incremental_key"
            )

        source_fields = urlparse(uri)
        source_params = parse_qs(source_fields.query)
        list_players = None
        if "players" in source_params:
            list_players = source_params["players"][0].split(",")
        else:
            list_players = [
                "MagnusCarlsen",
                "HikaruNakamura",
                "ArjunErigaisi",
                "IanNepomniachtchi",
            ]

        date_args = {}
        start_date = kwargs.get("interval_start")
        end_date = kwargs.get("interval_end")
        if start_date and end_date:
            if isinstance(start_date, date) and isinstance(end_date, date):
                date_args["start_month"] = start_date.strftime("%Y/%m")
                date_args["end_month"] = end_date.strftime("%Y/%m")

        table_mapping = {
            "profiles": "players_profiles",
            "games": "players_games",
            "archives": "players_archives",
        }

        if table not in table_mapping:
            raise ValueError(
                f"Resource '{table}' is not supported for Chess source yet, if you are interested in it please create a GitHub issue at https://github.com/bruin-data/ingestr"
            )

        from ingestr.src.chess import source

        return source(players=list_players, **date_args).with_resources(
            table_mapping[table]
        )


class StripeAnalyticsSource:
    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        if kwargs.get("incremental_key"):
            raise ValueError(
                "Stripe takes care of incrementality on its own, you should not provide incremental_key"
            )

        api_key = None
        source_field = urlparse(uri)
        source_params = parse_qs(source_field.query)
        api_key = source_params.get("api_key")

        if not api_key:
            raise ValueError("api_key in the URI is required to connect to Stripe")

        table = table.lower()

        from ingestr.src.stripe_analytics.settings import ENDPOINTS

        endpoint = None
        incremental = False
        sync = False

        table_fields = table.split(":")
        if len(table_fields) == 1:
            endpoint = table_fields[0]
        elif len(table_fields) == 2:
            endpoint = table_fields[0]
            sync = table_fields[1] == "sync"
        elif len(table_fields) == 3:
            endpoint = table_fields[0]
            sync = table_fields[1] == "sync"
            incremental = table_fields[2] == "incremental"
        else:
            raise ValueError(
                "Invalid Stripe table format. Expected: stripe:<endpoint> or stripe:<endpoint>:<sync> or stripe:<endpoint>:<sync>:<incremental>"
            )

        if incremental and not sync:
            raise ValueError("incremental loads must be used with sync loading")

        if incremental:
            from ingestr.src.stripe_analytics import incremental_stripe_source

            def nullable_date(date_str: Optional[str]):
                if date_str:
                    return ensure_pendulum_datetime(date_str)
                return None

            endpoint = ENDPOINTS[endpoint]
            return incremental_stripe_source(
                endpoints=[
                    endpoint,
                ],
                stripe_secret_key=api_key[0],
                initial_start_date=nullable_date(kwargs.get("interval_start", None)),
                end_date=nullable_date(kwargs.get("interval_end", None)),
            ).with_resources(endpoint)
        else:
            endpoint = ENDPOINTS[endpoint]
            if sync:
                from ingestr.src.stripe_analytics import stripe_source

                return stripe_source(
                    endpoints=[
                        endpoint,
                    ],
                    stripe_secret_key=api_key[0],
                ).with_resources(endpoint)
            else:
                from ingestr.src.stripe_analytics import async_stripe_source

                return async_stripe_source(
                    endpoints=[
                        endpoint,
                    ],
                    stripe_secret_key=api_key[0],
                    max_workers=kwargs.get("extract_parallelism", 4),
                ).with_resources(endpoint)

        raise ValueError(
            f"Resource '{table}' is not supported for stripe source yet, if you are interested in it please create a GitHub issue at https://github.com/bruin-data/ingestr"
        )


class FacebookAdsSource:
    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        # facebook_ads://?access_token=abcd&account_id=1234
        if kwargs.get("incremental_key"):
            raise ValueError(
                "Facebook Ads takes care of incrementality on its own, you should not provide incremental_key"
            )

        access_token = None
        account_id = None
        source_field = urlparse(uri)
        source_params = parse_qs(source_field.query)
        access_token = source_params.get("access_token")
        account_id = source_params.get("account_id")

        if not access_token:
            raise ValueError("access_token is required to connect to Facebook Ads.")

        from ingestr.src.facebook_ads import (
            facebook_ads_source,
            facebook_insights_source,
            facebook_insights_with_account_ids_source,
        )

        insights_max_wait_to_finish_seconds = source_params.get(
            "insights_max_wait_to_finish_seconds", [60 * 60 * 4]
        )
        insights_max_wait_to_start_seconds = source_params.get(
            "insights_max_wait_to_start_seconds", [60 * 30]
        )
        insights_max_async_sleep_seconds = source_params.get(
            "insights_max_async_sleep_seconds", [20]
        )

        endpoint = None
        table_account_ids = None

        if table in ["campaigns", "ad_sets", "ad_creatives", "ads", "leads"]:
            endpoint = table
        elif ":" in table and table.split(":")[0] in [
            "campaigns",
            "ad_sets",
            "ad_creatives",
            "ads",
            "leads",
        ]:
            parts = table.split(":")
            endpoint = parts[0]
            table_account_ids = [a.strip() for a in parts[1].split(",") if a.strip()]
        elif table == "facebook_insights":
            if not account_id:
                raise ValueError(
                    "account_id is required for facebook_insights. Provide it in the URI (?account_id=xxx) or use facebook_insights_with_account_ids:account_id1,account_id2"
                )
            return facebook_insights_source(
                access_token=access_token[0],
                account_id=account_id[0],
                start_date=kwargs.get("interval_start"),
                end_date=kwargs.get("interval_end"),
                insights_max_wait_to_finish_seconds=insights_max_wait_to_finish_seconds[
                    0
                ],
                insights_max_wait_to_start_seconds=insights_max_wait_to_start_seconds[
                    0
                ],
                insights_max_async_sleep_seconds=insights_max_async_sleep_seconds[0],
            ).with_resources("facebook_insights")
        elif table.startswith("facebook_insights_with_account_ids:"):
            parts = table.split(":")
            if len(parts) < 2:
                raise ValueError(
                    "Invalid facebook_insights_with_account_ids format. Expected: facebook_insights_with_account_ids:account_id1,account_id2"
                )

            multi_account_ids = [a.strip() for a in parts[1].split(",") if a.strip()]
            if not multi_account_ids:
                raise ValueError(
                    "At least one account_id must be provided in format: facebook_insights_with_account_ids:account_id1,account_id2"
                )

            from ingestr.src.facebook_ads.helpers import (
                parse_insights_table_to_source_kwargs,
            )

            source_kwargs = {
                "access_token": access_token[0],
                "account_ids": multi_account_ids,
                "start_date": kwargs.get("interval_start"),
                "end_date": kwargs.get("interval_end"),
                "insights_max_wait_to_finish_seconds": int(
                    insights_max_wait_to_finish_seconds[0]
                ),
                "insights_max_wait_to_start_seconds": int(
                    insights_max_wait_to_start_seconds[0]
                ),
                "insights_max_async_sleep_seconds": int(
                    insights_max_async_sleep_seconds[0]
                ),
            }

            if len(parts) > 2:
                remaining_table = ":".join(["facebook_insights"] + parts[2:])
                source_kwargs.update(
                    parse_insights_table_to_source_kwargs(remaining_table)
                )

            return facebook_insights_with_account_ids_source(
                **source_kwargs
            ).with_resources("facebook_insights")
        elif table.startswith("facebook_insights:"):
            # Parse custom breakdowns and metrics from table name
            # Supported formats:
            # facebook_insights:breakdown_type
            # facebook_insights:breakdown_type:metric1,metric2...
            parts = table.split(":")

            if len(parts) < 2 or len(parts) > 3:
                raise ValueError(
                    "Invalid facebook_insights format. Expected: facebook_insights:breakdown_type or facebook_insights:breakdown_type:metric1,metric2..."
                )

            breakdown_type = parts[1].strip()
            if not breakdown_type:
                raise ValueError(
                    "Breakdown type must be provided in format: facebook_insights:breakdown_type"
                )

            if not account_id:
                raise ValueError(
                    "account_id is required for facebook_insights. Provide it in the URI (?account_id=xxx) or use facebook_insights_with_account_ids:account_id1,account_id2"
                )

            # Validate breakdown type against available options from settings

            from ingestr.src.facebook_ads.helpers import (
                parse_insights_table_to_source_kwargs,
            )

            source_kwargs = {
                "access_token": access_token[0],
                "account_id": account_id[0],
                "start_date": kwargs.get("interval_start"),
                "end_date": kwargs.get("interval_end"),
            }

            source_kwargs.update(parse_insights_table_to_source_kwargs(table))
            return facebook_insights_source(**source_kwargs).with_resources(
                "facebook_insights"
            )
        else:
            raise ValueError(
                f"Resource '{table}' is not supported for Facebook Ads source yet, if you are interested in it please create a GitHub issue at https://github.com/bruin-data/ingestr"
            )

        final_account_ids = table_account_ids if table_account_ids else account_id

        if not final_account_ids:
            raise ValueError(
                "account_id is required. Provide it in the URI (?account_id=xxx) or in the table name (campaigns:xxx)"
            )

        return facebook_ads_source(
            access_token=access_token[0],
            account_id=final_account_ids
            if len(final_account_ids) > 1
            else final_account_ids[0],
        ).with_resources(endpoint)


class SlackSource:
    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        if kwargs.get("incremental_key"):
            raise ValueError(
                "Slack takes care of incrementality on its own, you should not provide incremental_key"
            )
        # slack://?api_key=<apikey>
        api_key = None
        source_field = urlparse(uri)
        source_query = parse_qs(source_field.query)
        api_key = source_query.get("api_key")

        if not api_key:
            raise ValueError("api_key in the URI is required to connect to Slack")

        endpoint = None
        msg_channels = None
        if table in ["channels", "users", "access_logs"]:
            endpoint = table
        elif table.startswith("messages"):
            channels_part = table.split(":")[1]
            msg_channels = channels_part.split(",")
            endpoint = "messages"
        else:
            raise ValueError(
                f"Resource '{table}' is not supported for slack source yet, if you are interested in it please create a GitHub issue at https://github.com/bruin-data/ingestr"
            )

        date_args = {}
        if kwargs.get("interval_start"):
            date_args["start_date"] = kwargs.get("interval_start")

        if kwargs.get("interval_end"):
            date_args["end_date"] = kwargs.get("interval_end")

        from ingestr.src.slack import slack_source

        return slack_source(
            access_token=api_key[0],
            table_per_channel=False,
            selected_channels=msg_channels,
            **date_args,
        ).with_resources(endpoint)


class HubspotSource:
    def handles_incrementality(self) -> bool:
        return False

    # hubspot://?api_key=<api_key>
    def dlt_source(self, uri: str, table: str, **kwargs):
        if kwargs.get("incremental_key"):
            raise ValueError(
                "Hubspot takes care of incrementality on its own, you should not provide incremental_key"
            )

        api_key = None
        source_parts = urlparse(uri)
        source_parmas = parse_qs(source_parts.query)
        api_key = source_parmas.get("api_key")

        if not api_key:
            raise ValueError("api_key in the URI is required to connect to Hubspot")

        endpoint = None

        from ingestr.src.hubspot import hubspot

        if table.startswith("custom:"):
            fields = table.split(":", 2)
            if len(fields) != 2 and len(fields) != 3:
                raise ValueError(
                    "Invalid Hubspot custom table format. Expected format: custom:<custom_object_type> or custom:<custom_object_type>:<associations>"
                )

            if len(fields) == 2:
                endpoint = fields[1]
            else:
                endpoint = f"{fields[1]}:{fields[2]}"

            return hubspot(
                api_key=api_key[0],
                custom_object=endpoint,
            ).with_resources("custom")

        elif table in [
            "contacts",
            "companies",
            "deals",
            "tickets",
            "products",
            "quotes",
            "schemas",
        ]:
            endpoint = table
        else:
            raise ValueError(
                f"Resource '{table}' is not supported for Hubspot source yet, if you are interested in it please create a GitHub issue at https://github.com/bruin-data/ingestr"
            )

        return hubspot(
            api_key=api_key[0],
        ).with_resources(endpoint)


class AirtableSource:
    def handles_incrementality(self) -> bool:
        return False

    # airtable://?access_token=<access_token>&base_id=<base_id>

    def dlt_source(self, uri: str, table: str, **kwargs):
        if kwargs.get("incremental_key"):
            raise ValueError("Incremental loads are not supported for Airtable")

        if not table:
            raise ValueError("Source table is required to connect to Airtable")

        source_parts = urlparse(uri)
        source_fields = parse_qs(source_parts.query)
        access_token = source_fields.get("access_token")

        if not access_token:
            raise ValueError(
                "access_token in the URI is required to connect to Airtable"
            )

        base_id = source_fields.get("base_id", [None])[0]
        clean_table = table

        table_fields = table.split("/")
        if len(table_fields) == 2:
            clean_table = table_fields[1]
            if not base_id:
                base_id = table_fields[0]

        if not base_id:
            raise ValueError("base_id in the URI is required to connect to Airtable")

        from ingestr.src.airtable import airtable_source

        return airtable_source(
            base_id=base_id, table_names=[clean_table], access_token=access_token[0]
        )


class KlaviyoSource:
    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        if kwargs.get("incremental_key"):
            raise ValueError(
                "klaviyo_source takes care of incrementality on its own, you should not provide incremental_key"
            )

        source_fields = urlparse(uri)
        source_params = parse_qs(source_fields.query)
        api_key = source_params.get("api_key")

        if not api_key:
            raise ValueError("api_key in the URI is required to connect to klaviyo")

        resource = None
        if table in [
            "events",
            "profiles",
            "campaigns",
            "metrics",
            "tags",
            "coupons",
            "catalog-variants",
            "catalog-categories",
            "catalog-items",
            "forms",
            "lists",
            "images",
            "segments",
            "flows",
            "templates",
        ]:
            resource = table
        else:
            raise ValueError(
                f"Resource '{table}' is not supported for Klaviyo source yet, if you are interested in it please create a GitHub issue at https://github.com/bruin-data/ingestr"
            )

        start_date = kwargs.get("interval_start") or "2000-01-01"

        from ingestr.src.klaviyo import klaviyo_source

        return klaviyo_source(
            api_key=api_key[0],
            start_date=start_date,
        ).with_resources(resource)


class MixpanelSource:
    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        if kwargs.get("incremental_key"):
            raise ValueError(
                "Mixpanel takes care of incrementality on its own, you should not provide incremental_key"
            )

        parsed = urlparse(uri)
        params = parse_qs(parsed.query)
        username = params.get("username")
        password = params.get("password")
        project_id = params.get("project_id")
        server = params.get("server", ["eu"])

        if not username or not password or not project_id:
            raise ValueError(
                "username, password, project_id are required to connect to Mixpanel"
            )

        if table not in ["events", "profiles"]:
            raise ValueError(
                f"Resource '{table}' is not supported for Mixpanel source yet, if you are interested in it please create a GitHub issue at https://github.com/bruin-data/ingestr"
            )

        start_date = kwargs.get("interval_start")
        if start_date:
            start_date = ensure_pendulum_datetime(start_date).in_timezone("UTC")
        else:
            start_date = pendulum.datetime(2020, 1, 1).in_timezone("UTC")

        end_date = kwargs.get("interval_end")
        if end_date:
            end_date = ensure_pendulum_datetime(end_date).in_timezone("UTC")
        else:
            end_date = pendulum.now().in_timezone("UTC")

        from ingestr.src.mixpanel import mixpanel_source

        return mixpanel_source(
            username=username[0],
            password=password[0],
            project_id=project_id[0],
            start_date=start_date,
            end_date=end_date,
            server=server[0],
        ).with_resources(table)


class KafkaSource:
    def handles_incrementality(self) -> bool:
        return False

    def dlt_source(self, uri: str, table: str, **kwargs):
        # kafka://?bootstrap_servers=localhost:9092&group_id=test_group&security_protocol=SASL_SSL&sasl_mechanisms=PLAIN&sasl_username=example_username&sasl_password=example_secret
        source_fields = urlparse(uri)
        source_params = parse_qs(source_fields.query)

        bootstrap_servers = source_params.get("bootstrap_servers")
        group_id = source_params.get("group_id")
        security_protocol = source_params.get("security_protocol", [])
        sasl_mechanisms = source_params.get("sasl_mechanisms", [])
        sasl_username = source_params.get("sasl_username", [])
        sasl_password = source_params.get("sasl_password", [])
        batch_size = source_params.get("batch_size", [3000])
        batch_timeout = source_params.get("batch_timeout", [3])

        if not bootstrap_servers:
            raise ValueError(
                "bootstrap_servers in the URI is required to connect to kafka"
            )

        if not group_id:
            raise ValueError("group_id in the URI is required to connect to kafka")

        start_date = kwargs.get("interval_start")
        from ingestr.src.kafka import kafka_consumer
        from ingestr.src.kafka.helpers import KafkaCredentials

        return kafka_consumer(
            topics=[table],
            credentials=KafkaCredentials(
                bootstrap_servers=bootstrap_servers[0],
                group_id=group_id[0],
                security_protocol=(
                    security_protocol[0] if len(security_protocol) > 0 else None
                ),  # type: ignore
                sasl_mechanisms=(
                    sasl_mechanisms[0] if len(sasl_mechanisms) > 0 else None
                ),  # type: ignore
                sasl_username=sasl_username[0] if len(sasl_username) > 0 else None,  # type: ignore
                sasl_password=sasl_password[0] if len(sasl_password) > 0 else None,  # type: ignore
            ),
            start_from=start_date,
            batch_size=int(batch_size[0]),
            batch_timeout=int(batch_timeout[0]),
        )


class AdjustSource:
    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        if kwargs.get("incremental_key") and not table.startswith("custom:"):
            raise ValueError(
                "Adjust takes care of incrementality on its own, you should not provide incremental_key"
            )

        source_part = urlparse(uri)
        source_params = parse_qs(source_part.query)
        api_key = source_params.get("api_key")

        if not api_key:
            raise ValueError("api_key in the URI is required to connect to Adjust")

        lookback_days = int(source_params.get("lookback_days", [30])[0])

        start_date = (
            pendulum.now()
            .replace(hour=0, minute=0, second=0, microsecond=0)
            .subtract(days=lookback_days)
        )
        if kwargs.get("interval_start"):
            start_date = (
                ensure_pendulum_datetime(str(kwargs.get("interval_start")))
                .replace(hour=0, minute=0, second=0, microsecond=0)
                .subtract(days=lookback_days)
            )

        end_date = pendulum.now()
        if kwargs.get("interval_end"):
            end_date = ensure_pendulum_datetime(str(kwargs.get("interval_end")))

        from ingestr.src.adjust import REQUIRED_CUSTOM_DIMENSIONS, adjust_source
        from ingestr.src.adjust.adjust_helpers import parse_filters

        dimensions = None
        metrics = None
        filters = []
        if table.startswith("custom:"):
            fields = table.split(":", 3)
            if len(fields) != 3 and len(fields) != 4:
                raise ValueError(
                    "Invalid Adjust custom table format. Expected format: custom:<dimensions>,<metrics> or custom:<dimensions>:<metrics>:<filters>"
                )

            dimensions = fields[1].split(",")
            metrics = fields[2].split(",")
            table = "custom"

            found = False
            for dimension in dimensions:
                if dimension in REQUIRED_CUSTOM_DIMENSIONS:
                    found = True
                    break

            if not found:
                raise ValueError(
                    f"At least one of the required dimensions is missing for custom Adjust report: {REQUIRED_CUSTOM_DIMENSIONS}"
                )

            if len(fields) == 4:
                filters_raw = fields[3]
                filters = parse_filters(filters_raw)

        src = adjust_source(
            start_date=start_date,
            end_date=end_date,
            api_key=api_key[0],
            dimensions=dimensions,
            metrics=metrics,
            merge_key=kwargs.get("merge_key"),
            filters=filters,
        )

        return src.with_resources(table)


class AppsflyerSource:
    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        from ingestr.src.appsflyer import appsflyer_source

        if kwargs.get("incremental_key"):
            raise ValueError(
                "Appsflyer_Source takes care of incrementality on its own, you should not provide incremental_key"
            )

        source_fields = urlparse(uri)
        source_params = parse_qs(source_fields.query)
        api_key = source_params.get("api_key")

        if not api_key:
            raise ValueError("api_key in the URI is required to connect to Appsflyer")

        start_date = kwargs.get("interval_start")
        end_date = kwargs.get("interval_end")
        dimensions = []
        metrics = []
        if table.startswith("custom:"):
            fields = table.split(":", 3)
            if len(fields) != 3:
                raise ValueError(
                    "Invalid Adjust custom table format. Expected format: custom:<dimensions>:<metrics>"
                )
            dimensions = fields[1].split(",")
            metrics = fields[2].split(",")
            table = "custom"

        return appsflyer_source(
            api_key=api_key[0],
            start_date=start_date.strftime("%Y-%m-%d") if start_date else None,  # type: ignore
            end_date=end_date.strftime("%Y-%m-%d") if end_date else None,  # type: ignore
            dimensions=dimensions,
            metrics=metrics,
        ).with_resources(table)


class ZendeskSource:
    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        if kwargs.get("incremental_key"):
            raise ValueError(
                "Zendesk takes care of incrementality on its own, you should not provide incremental_key"
            )

        interval_start = kwargs.get("interval_start")
        interval_end = kwargs.get("interval_end")
        start_date = (
            interval_start.strftime("%Y-%m-%d") if interval_start else "2000-01-01"
        )
        end_date = interval_end.strftime("%Y-%m-%d") if interval_end else None

        source_fields = urlparse(uri)
        subdomain = source_fields.hostname
        if not subdomain:
            raise ValueError("Subdomain is required to connect with Zendesk")

        from ingestr.src.zendesk import zendesk_chat, zendesk_support, zendesk_talk
        from ingestr.src.zendesk.helpers.credentials import (
            ZendeskCredentialsOAuth,
            ZendeskCredentialsToken,
        )

        if not source_fields.username and source_fields.password:
            oauth_token = source_fields.password
            if not oauth_token:
                raise ValueError(
                    "oauth_token in the URI is required to connect to Zendesk"
                )
            credentials = ZendeskCredentialsOAuth(
                subdomain=subdomain, oauth_token=oauth_token
            )
        elif source_fields.username and source_fields.password:
            email = source_fields.username
            api_token = source_fields.password
            if not email or not api_token:
                raise ValueError(
                    "Both email and token must be provided to connect to Zendesk"
                )
            credentials = ZendeskCredentialsToken(
                subdomain=subdomain, email=email, token=api_token
            )
        else:
            raise ValueError("Invalid URI format")

        if table in [
            "ticket_metrics",
            "users",
            "ticket_metric_events",
            "ticket_forms",
            "tickets",
            "targets",
            "activities",
            "brands",
            "groups",
            "organizations",
            "sla_policies",
            "automations",
        ]:
            return zendesk_support(
                credentials=credentials, start_date=start_date, end_date=end_date
            ).with_resources(table)
        elif table in [
            "greetings",
            "settings",
            "addresses",
            "legs_incremental",
            "calls",
            "phone_numbers",
            "lines",
            "agents_activity",
        ]:
            return zendesk_talk(
                credentials=credentials, start_date=start_date, end_date=end_date
            ).with_resources(table)
        elif table in ["chats"]:
            return zendesk_chat(
                credentials=credentials, start_date=start_date, end_date=end_date
            ).with_resources(table)
        else:
            raise ValueError(
                f"Resource '{table}' is not supported for Zendesk source yet, if you are interested in it please create a GitHub issue at https://github.com/bruin-data/ingestr"
            )


class S3Source:
    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        if kwargs.get("incremental_key"):
            raise ValueError(
                "S3 takes care of incrementality on its own, you should not provide incremental_key"
            )

        parsed_uri = urlparse(uri)
        source_fields = parse_qs(parsed_uri.query)
        access_key_id = source_fields.get("access_key_id")
        if not access_key_id:
            raise ValueError("access_key_id is required to connect to S3")

        secret_access_key = source_fields.get("secret_access_key")
        if not secret_access_key:
            raise ValueError("secret_access_key is required to connect to S3")

        bucket_name, path_to_file = blob.parse_uri(parsed_uri, table)
        if not bucket_name or not path_to_file:
            raise InvalidBlobTableError("S3")

        bucket_url = f"s3://{bucket_name}/"

        import s3fs  # type: ignore

        fs = s3fs.S3FileSystem(
            key=access_key_id[0],
            secret=secret_access_key[0],
        )

        endpoint: Optional[str] = None
        if "#" in table:
            _, endpoint = table.split("#")
            if endpoint not in ["csv", "jsonl", "parquet"]:
                raise ValueError(
                    "S3 Source only supports specific formats files: csv, jsonl, parquet"
                )
            endpoint = f"read_{endpoint}"
        else:
            try:
                endpoint = blob.parse_endpoint(path_to_file)
            except blob.UnsupportedEndpointError:
                raise ValueError(
                    "S3 Source only supports specific formats files: csv, jsonl, parquet"
                )
            except Exception as e:
                raise ValueError(
                    f"Failed to parse endpoint from path: {path_to_file}"
                ) from e

        from ingestr.src.filesystem import readers

        return readers(bucket_url, fs, path_to_file).with_resources(endpoint)


class TikTokSource:
    # tittok://?access_token=<access_token>&advertiser_id=<advertiser_id>
    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        if kwargs.get("incremental_key"):
            raise ValueError(
                "TikTok takes care of incrementality on its own, you should not provide incremental_key"
            )

        endpoint = "custom_reports"

        parsed_uri = urlparse(uri)
        source_fields = parse_qs(parsed_uri.query)

        access_token = source_fields.get("access_token")
        if not access_token:
            raise ValueError("access_token is required to connect to TikTok")

        timezone = "UTC"
        if source_fields.get("timezone") is not None:
            timezone = source_fields.get("timezone")[0]  # type: ignore

        advertiser_ids = source_fields.get("advertiser_ids")
        if not advertiser_ids:
            raise ValueError("advertiser_ids is required to connect to TikTok")

        advertiser_ids = advertiser_ids[0].replace(" ", "").split(",")

        start_date = pendulum.now().subtract(days=30).in_tz(timezone)
        end_date = ensure_pendulum_datetime(pendulum.now()).in_tz(timezone)

        interval_start = kwargs.get("interval_start")
        if interval_start is not None:
            start_date = ensure_pendulum_datetime(interval_start).in_tz(timezone)

        interval_end = kwargs.get("interval_end")
        if interval_end is not None:
            end_date = ensure_pendulum_datetime(interval_end).in_tz(timezone)

        page_size = min(1000, kwargs.get("page_size", 1000))

        if table.startswith("custom:"):
            fields = table.split(":", 3)
            if len(fields) != 3 and len(fields) != 4:
                raise ValueError(
                    "Invalid TikTok custom table format. Expected format: custom:<dimensions>,<metrics> or custom:<dimensions>:<metrics>:<filters>"
                )

            dimensions = fields[1].replace(" ", "").split(",")
            if (
                "campaign_id" not in dimensions
                and "adgroup_id" not in dimensions
                and "ad_id" not in dimensions
            ):
                raise ValueError(
                    "TikTok API requires at least one ID dimension, please use one of the following dimensions: [campaign_id, adgroup_id, ad_id]"
                )

            if "advertiser_id" in dimensions:
                dimensions.remove("advertiser_id")

            metrics = fields[2].replace(" ", "").split(",")
            filtering_param = False
            filter_name = ""
            filter_value = []
            if len(fields) == 4:

                def parse_filters(filters_raw: str) -> dict:
                    # Parse filter string like "key1=value1,key2=value2,value3,value4"
                    filters = {}
                    current_key = None

                    for item in filters_raw.split(","):
                        if "=" in item:
                            # Start of a new key-value pair
                            key, value = item.split("=")
                            filters[key] = [value]  # Always start with a list
                            current_key = key
                        elif current_key is not None:
                            # Additional value for the current key
                            filters[current_key].append(item)

                    # Convert single-item lists to simple values
                    return {k: v[0] if len(v) == 1 else v for k, v in filters.items()}

                filtering_param = True
                filters = parse_filters(fields[3])
                if len(filters) > 1:
                    raise ValueError(
                        "Only one filter is allowed for TikTok custom reports"
                    )
                filter_name = list(filters.keys())[0]
                filter_value = list(map(int, filters[list(filters.keys())[0]]))

        from ingestr.src.tiktok_ads import tiktok_source

        return tiktok_source(
            start_date=start_date,
            end_date=end_date,
            access_token=access_token[0],
            advertiser_ids=advertiser_ids,
            timezone=timezone,
            dimensions=dimensions,
            metrics=metrics,
            page_size=page_size,
            filter_name=filter_name,
            filter_value=filter_value,
            filtering_param=filtering_param,
        ).with_resources(endpoint)


class AsanaSource:
    resources = [
        "workspaces",
        "projects",
        "sections",
        "tags",
        "tasks",
        "stories",
        "teams",
        "users",
    ]

    def handles_incrementality(self) -> bool:
        return False

    def dlt_source(self, uri: str, table: str, **kwargs):
        parsed_uri = urlparse(uri)
        params = parse_qs(parsed_uri.query)

        workspace = parsed_uri.hostname
        access_token = params.get("access_token")

        if not workspace:
            raise ValueError("workspace ID must be specified in the URI")

        if not access_token:
            raise ValueError("access_token is required for connecting to Asana")

        if table not in self.resources:
            raise ValueError(
                f"Resource '{table}' is not supported for Asana source yet, if you are interested in it please create a GitHub issue at https://github.com/bruin-data/ingestr"
            )

        import dlt

        from ingestr.src.asana_source import asana_source

        dlt.secrets["sources.asana_source.access_token"] = access_token[0]

        src = asana_source()
        src.workspaces.add_filter(lambda w: w["gid"] == workspace)
        return src.with_resources(table)


class JiraSource:
    resources = [
        "projects",
        "issues",
        "users",
        "issue_types",
        "statuses",
        "priorities",
        "resolutions",
        "project_versions",
        "project_components",
        "events",
    ]

    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        parsed_uri = urlparse(uri)
        params = parse_qs(parsed_uri.query)

        base_url = f"https://{parsed_uri.netloc}"
        email = params.get("email")
        api_token = params.get("api_token")

        if not email:
            raise ValueError("email must be specified in the URI query parameters")

        if not api_token:
            raise ValueError("api_token is required for connecting to Jira")

        flags = {
            "skip_archived": False,
        }
        if ":" in table:
            table, rest = table.split(":", 1)  # type: ignore
            for k in rest.split(":"):
                flags[k] = True

        if table not in self.resources:
            raise ValueError(
                f"Resource '{table}' is not supported for Jira source yet, if you are interested in it please create a GitHub issue at https://github.com/bruin-data/ingestr"
            )

        import dlt

        from ingestr.src.jira_source import jira_source

        dlt.secrets["sources.jira_source.base_url"] = base_url
        dlt.secrets["sources.jira_source.email"] = email[0]
        dlt.secrets["sources.jira_source.api_token"] = api_token[0]

        src = jira_source()
        if flags["skip_archived"]:
            src.projects.add_filter(lambda p: not p.get("archived", False))
        return src.with_resources(table)


class DynamoDBSource:
    AWS_ENDPOINT_PATTERN = re.compile(".*\.(.+)\.amazonaws\.com")

    def infer_aws_region(self, uri: ParseResult) -> Optional[str]:
        # try to infer from URI
        matches = self.AWS_ENDPOINT_PATTERN.match(uri.netloc)
        if matches is not None:
            return matches[1]

        # else obtain region from query string
        region = parse_qs(uri.query).get("region")
        if region is None:
            return None
        return region[0]

    def get_endpoint_url(self, url: ParseResult) -> str:
        if self.AWS_ENDPOINT_PATTERN.match(url.netloc) is not None:
            return f"https://{url.hostname}"
        return f"http://{url.netloc}"

    def handles_incrementality(self) -> bool:
        return False

    def dlt_source(self, uri: str, table: str, **kwargs):
        parsed_uri = urlparse(uri)

        region = self.infer_aws_region(parsed_uri)
        if not region:
            raise ValueError("region is required to connect to Dynamodb")

        qs = parse_qs(parsed_uri.query)
        access_key = qs.get("access_key_id")

        if not access_key:
            raise ValueError("access_key_id is required to connect to Dynamodb")

        secret_key = qs.get("secret_access_key")
        if not secret_key:
            raise ValueError("secret_access_key is required to connect to Dynamodb")

        from dlt.common.configuration.specs import AwsCredentials
        from dlt.common.typing import TSecretStrValue

        creds = AwsCredentials(
            aws_access_key_id=access_key[0],
            aws_secret_access_key=TSecretStrValue(secret_key[0]),
            region_name=region,
            endpoint_url=self.get_endpoint_url(parsed_uri),
        )

        incremental = None
        incremental_key = kwargs.get("incremental_key")

        from ingestr.src.dynamodb import dynamodb
        from ingestr.src.time import isotime

        if incremental_key:
            incremental = dlt_incremental(
                incremental_key.strip(),
                initial_value=isotime(kwargs.get("interval_start")),
                end_value=isotime(kwargs.get("interval_end")),
                range_end="closed",
                range_start="closed",
            )

        # bug: we never validate table.
        return dynamodb(table, creds, incremental)


class DoceboSource:
    def handles_incrementality(self) -> bool:
        return False

    def dlt_source(self, uri: str, table: str, **kwargs):
        # docebo://?base_url=https://yourcompany.docebosaas.com&client_id=xxx&client_secret=xxx
        # Optional: &username=xxx&password=xxx for password grant type

        if kwargs.get("incremental_key"):
            raise ValueError("Incremental loads are not yet supported for Docebo")

        parsed_uri = urlparse(uri)
        source_params = parse_qs(parsed_uri.query)

        base_url = source_params.get("base_url")
        if not base_url:
            raise ValueError("base_url is required to connect to Docebo")

        client_id = source_params.get("client_id")
        if not client_id:
            raise ValueError("client_id is required to connect to Docebo")

        client_secret = source_params.get("client_secret")
        if not client_secret:
            raise ValueError("client_secret is required to connect to Docebo")

        # Username and password are optional (uses client_credentials grant if not provided)
        username = source_params.get("username", [None])[0]
        password = source_params.get("password", [None])[0]

        # Supported tables
        supported_tables = [
            "users",
            "courses",
            "user_fields",
            "branches",
            "groups",
            "group_members",
            "course_fields",
            "learning_objects",
            "learning_plans",
            "learning_plan_enrollments",
            "learning_plan_course_enrollments",
            "course_enrollments",
            "sessions",
            "categories",
            "certifications",
            "external_training",
            "survey_answers",
        ]
        if table not in supported_tables:
            raise ValueError(
                f"Resource '{table}' is not supported for Docebo source. Supported tables: {', '.join(supported_tables)}"
            )

        from ingestr.src.docebo import docebo_source

        return docebo_source(
            base_url=base_url[0],
            client_id=client_id[0],
            client_secret=client_secret[0],
            username=username,
            password=password,
        ).with_resources(table)


class GoogleAnalyticsSource:
    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        import ingestr.src.google_analytics.helpers as helpers

        if kwargs.get("incremental_key"):
            raise ValueError(
                "Google Analytics takes care of incrementality on its own, you should not provide incremental_key"
            )

        result = helpers.parse_google_analytics_uri(uri)
        credentials = result["credentials"]
        property_id = result["property_id"]

        fields = table.split(":")
        if len(fields) != 3 and len(fields) != 4:
            raise ValueError(
                "Invalid table format. Expected format: <report_type>:<dimensions>:<metrics> or <report_type>:<dimensions>:<metrics>:<minute_ranges>"
            )

        report_type = fields[0]
        if report_type not in ["custom", "realtime"]:
            raise ValueError(
                "Invalid report type. Expected format: <report_type>:<dimensions>:<metrics>. Available report types: custom, realtime"
            )

        dimensions = fields[1].replace(" ", "").split(",")
        metrics = fields[2].replace(" ", "").split(",")

        minute_range_objects = []
        if len(fields) == 4:
            minute_range_objects = (
                helpers.convert_minutes_ranges_to_minute_range_objects(fields[3])
            )

        datetime = ""
        resource_name = fields[0].lower()
        if resource_name == "custom":
            for dimension_datetime in ["date", "dateHourMinute", "dateHour"]:
                if dimension_datetime in dimensions:
                    datetime = dimension_datetime
                    break
            else:
                raise ValueError(
                    "You must provide at least one dimension: [dateHour, dateHourMinute, date]"
                )

        queries = [
            {
                "resource_name": resource_name,
                "dimensions": dimensions,
                "metrics": metrics,
            }
        ]

        start_date = pendulum.now().subtract(days=30).start_of("day")
        if kwargs.get("interval_start") is not None:
            start_date = pendulum.instance(kwargs.get("interval_start"))  # type: ignore

        end_date = pendulum.now()
        if kwargs.get("interval_end") is not None:
            end_date = pendulum.instance(kwargs.get("interval_end"))  # type: ignore

        from ingestr.src.google_analytics import google_analytics

        return google_analytics(
            property_id=property_id,
            start_date=start_date,
            end_date=end_date,
            datetime_dimension=datetime,
            queries=queries,
            credentials=credentials,
            minute_range_objects=minute_range_objects if minute_range_objects else None,
        ).with_resources(resource_name)


class GitHubSource:
    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        if kwargs.get("incremental_key"):
            raise ValueError(
                "Github takes care of incrementality on its own, you should not provide incremental_key"
            )
        # github://?access_token=<access_token>&owner=<owner>&repo=<repo>
        parsed_uri = urlparse(uri)
        source_fields = parse_qs(parsed_uri.query)

        owner = source_fields.get("owner", [None])[0]
        if not owner:
            raise ValueError(
                "owner of the repository is required to connect with GitHub"
            )

        repo = source_fields.get("repo", [None])[0]
        if not repo:
            raise ValueError(
                "repo variable is required to retrieve data for a specific repository from GitHub."
            )

        access_token = source_fields.get("access_token", [""])[0]

        from ingestr.src.github import (
            github_reactions,
            github_repo_events,
            github_stargazers,
        )

        if table in ["issues", "pull_requests"]:
            return github_reactions(
                owner=owner, name=repo, access_token=access_token
            ).with_resources(table)
        elif table == "repo_events":
            start_date = kwargs.get("interval_start") or pendulum.now().subtract(
                days=30
            )
            end_date = kwargs.get("interval_end") or None

            if isinstance(start_date, str):
                start_date = pendulum.parse(start_date)
            if isinstance(end_date, str):
                end_date = pendulum.parse(end_date)

            return github_repo_events(
                owner=owner,
                name=repo,
                access_token=access_token,
                start_date=start_date,
                end_date=end_date,
            )
        elif table == "stargazers":
            return github_stargazers(owner=owner, name=repo, access_token=access_token)
        else:
            raise ValueError(
                f"Resource '{table}' is not supported for GitHub source yet, if you are interested in it please create a GitHub issue at https://github.com/bruin-data/ingestr"
            )


class AppleAppStoreSource:
    def handles_incrementality(self) -> bool:
        return True

    def init_client(
        self,
        key_id: str,
        issuer_id: str,
        key_path: Optional[List[str]],
        key_base64: Optional[List[str]],
    ):
        key = None
        if key_path is not None:
            with open(key_path[0]) as f:
                key = f.read()
        else:
            key = base64.b64decode(key_base64[0]).decode()  # type: ignore

        from ingestr.src.appstore.client import AppStoreConnectClient

        return AppStoreConnectClient(key.encode(), key_id, issuer_id)

    def dlt_source(self, uri: str, table: str, **kwargs):
        if kwargs.get("incremental_key"):
            raise ValueError(
                "App Store takes care of incrementality on its own, you should not provide incremental_key"
            )
        parsed_uri = urlparse(uri)
        params = parse_qs(parsed_uri.query)

        key_id = params.get("key_id")
        if key_id is None:
            raise MissingValueError("key_id", "App Store")

        key_path = params.get("key_path")
        key_base64 = params.get("key_base64")
        key_available = any(
            map(
                lambda x: x is not None,
                [key_path, key_base64],
            )
        )
        if key_available is False:
            raise MissingValueError("key_path or key_base64", "App Store")

        issuer_id = params.get("issuer_id")
        if issuer_id is None:
            raise MissingValueError("issuer_id", "App Store")

        client = self.init_client(key_id[0], issuer_id[0], key_path, key_base64)

        app_ids = params.get("app_id")
        if ":" in table:
            intended_table, app_ids_override = table.split(":", maxsplit=1)
            app_ids = app_ids_override.split(",")
            table = intended_table

        if app_ids is None:
            raise MissingValueError("app_id", "App Store")

        from ingestr.src.appstore import app_store

        src = app_store(
            client,
            app_ids,
            start_date=kwargs.get(
                "interval_start", datetime.now() - timedelta(days=30)
            ),
            end_date=kwargs.get("interval_end"),
        )

        if table not in src.resources:
            raise UnsupportedResourceError(table, "AppStore")

        return src.with_resources(table)


class GCSSource:
    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        if kwargs.get("incremental_key"):
            raise ValueError(
                "GCS takes care of incrementality on its own, you should not provide incremental_key"
            )

        parsed_uri = urlparse(uri)
        params = parse_qs(parsed_uri.query)

        bucket_name, path_to_file = blob.parse_uri(parsed_uri, table)
        if not bucket_name or not path_to_file:
            raise InvalidBlobTableError("GCS")

        bucket_url = f"gs://{bucket_name}"

        credentials_path = params.get("credentials_path")
        credentials_base64 = params.get("credentials_base64")
        credentials_available = any(
            map(
                lambda x: x is not None,
                [credentials_path, credentials_base64],
            )
        )
        if credentials_available is False:
            raise MissingValueError("credentials_path or credentials_base64", "GCS")

        credentials = None
        if credentials_path:
            credentials = credentials_path[0]
        else:
            credentials = json.loads(base64.b64decode(credentials_base64[0]).decode())  # type: ignore

        # There's a compatiblity issue between google-auth, dlt and gcsfs
        # that makes it difficult to use google.oauth2.service_account.Credentials
        # (The RECOMMENDED way of passing service account credentials)
        # directly with gcsfs. As a workaround, we construct the GCSFileSystem
        # and pass it directly to filesystem.readers.
        import gcsfs  # type: ignore

        fs = gcsfs.GCSFileSystem(
            token=credentials,
        )

        try:
            endpoint = blob.parse_endpoint(path_to_file)
        except blob.UnsupportedEndpointError:
            raise ValueError(
                "GCS Source only supports specific formats files: csv, jsonl, parquet"
            )
        except Exception as e:
            raise ValueError(
                f"Failed to parse endpoint from path: {path_to_file}"
            ) from e

        from ingestr.src.filesystem import readers

        return readers(bucket_url, fs, path_to_file).with_resources(endpoint)


class GoogleAdsSource:
    def handles_incrementality(self) -> bool:
        return True

    def init_client(self, params: Dict[str, List[str]]):
        from google.ads.googleads.client import GoogleAdsClient  # type: ignore

        dev_token = params.get("dev_token")
        if dev_token is None or len(dev_token) == 0:
            raise MissingValueError("dev_token", "Google Ads")

        credentials_path = params.get("credentials_path")
        credentials_base64 = params.get("credentials_base64")
        credentials_available = any(
            map(
                lambda x: x is not None,
                [credentials_path, credentials_base64],
            )
        )
        if credentials_available is False:
            raise MissingValueError(
                "credentials_path or credentials_base64", "Google Ads"
            )

        path = None
        fd = None
        if credentials_path:
            path = credentials_path[0]
        else:
            (fd, path) = tempfile.mkstemp(prefix="secret-")
            secret = base64.b64decode(credentials_base64[0])  # type: ignore
            os.write(fd, secret)
            os.close(fd)

        conf = {
            "json_key_file_path": path,
            "use_proto_plus": True,
            "developer_token": dev_token[0],
        }
        try:
            client = GoogleAdsClient.load_from_dict(conf)
        finally:
            if fd is not None:
                os.remove(path)

        return client

    def dlt_source(self, uri: str, table: str, **kwargs):
        if kwargs.get("incremental_key") is not None:
            raise ValueError(
                "Google Ads takes care of incrementality on its own, you should not provide incremental_key"
            )

        parsed_uri = urlparse(uri)

        customer_id = parsed_uri.hostname
        if not customer_id:
            raise MissingValueError("customer_id", "Google Ads")

        params = parse_qs(parsed_uri.query)

        client = self.init_client(params)

        start_date = kwargs.get("interval_start") or datetime.now(
            tz=timezone.utc
        ) - timedelta(days=30)
        end_date = kwargs.get("interval_end")

        # most combinations of explict start/end dates are automatically handled.
        # however, in the scenario where only the end date is provided, we need to
        # calculate the start date based on the end date.
        if (
            kwargs.get("interval_end") is not None
            and kwargs.get("interval_start") is None
        ):
            start_date = end_date - timedelta(days=30)  # type: ignore

        report_spec = None
        if table.startswith("daily:"):
            report_spec = table
            table = "daily_report"

        from ingestr.src.google_ads import google_ads

        src = google_ads(
            client,
            customer_id,
            report_spec,
            start_date=start_date,
            end_date=end_date,
        )

        if table not in src.resources:
            raise UnsupportedResourceError(table, "Google Ads")

        return src.with_resources(table)


class LinkedInAdsSource:
    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        if kwargs.get("incremental_key"):
            raise ValueError(
                "LinkedIn Ads takes care of incrementality on its own, you should not provide incremental_key"
            )

        parsed_uri = urlparse(uri)
        source_fields = parse_qs(parsed_uri.query)

        access_token = source_fields.get("access_token")
        if not access_token:
            raise ValueError("access_token is required to connect to LinkedIn Ads")

        account_ids = source_fields.get("account_ids")

        if not account_ids:
            raise ValueError("account_ids is required to connect to LinkedIn Ads")
        account_ids = account_ids[0].replace(" ", "").split(",")

        interval_start = kwargs.get("interval_start")
        interval_end = kwargs.get("interval_end")
        start_date = (
            ensure_pendulum_datetime(interval_start).date()
            if interval_start
            else pendulum.datetime(2018, 1, 1).date()
        )
        end_date = (
            ensure_pendulum_datetime(interval_end).date() if interval_end else None
        )

        fields = table.split(":")
        if len(fields) != 3:
            raise ValueError(
                "Invalid table format. Expected format: custom:<dimensions>:<metrics>"
            )

        dimensions = fields[1].replace(" ", "").split(",")
        dimensions = [item for item in dimensions if item.strip()]
        if (
            "campaign" not in dimensions
            and "creative" not in dimensions
            and "account" not in dimensions
        ):
            raise ValueError(
                "'campaign', 'creative' or 'account' is required to connect to LinkedIn Ads, please provide at least one of these dimensions."
            )
        if "date" not in dimensions and "month" not in dimensions:
            raise ValueError(
                "'date' or 'month' is required to connect to LinkedIn Ads, please provide at least one of these dimensions."
            )

        from ingestr.src.linkedin_ads import linked_in_ads_source
        from ingestr.src.linkedin_ads.dimension_time_enum import (
            Dimension,
            TimeGranularity,
        )

        if "date" in dimensions:
            time_granularity = TimeGranularity.daily
            dimensions.remove("date")
        else:
            time_granularity = TimeGranularity.monthly
            dimensions.remove("month")

        dimension = Dimension[dimensions[0]]

        metrics = fields[2].replace(" ", "").split(",")
        metrics = [item for item in metrics if item.strip()]
        if "dateRange" not in metrics:
            metrics.append("dateRange")
        if "pivotValues" not in metrics:
            metrics.append("pivotValues")

        return linked_in_ads_source(
            start_date=start_date,
            end_date=end_date,
            access_token=access_token[0],
            account_ids=account_ids,
            dimension=dimension,
            metrics=metrics,
            time_granularity=time_granularity,
        ).with_resources("custom_reports")


class ClickupSource:
    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        if kwargs.get("incremental_key"):
            raise ValueError(
                "ClickUp takes care of incrementality on its own, you should not provide incremental_key"
            )

        parsed_uri = urlparse(uri)
        params = parse_qs(parsed_uri.query)
        api_token = params.get("api_token")

        if api_token is None:
            raise MissingValueError("api_token", "ClickUp")

        interval_start = kwargs.get("interval_start")
        interval_end = kwargs.get("interval_end")
        start_date = (
            ensure_pendulum_datetime(interval_start).in_timezone("UTC")
            if interval_start
            else pendulum.datetime(2020, 1, 1, tz="UTC")
        )
        end_date = (
            ensure_pendulum_datetime(interval_end).in_timezone("UTC")
            if interval_end
            else None
        )

        from ingestr.src.clickup import clickup_source

        if table not in {"user", "teams", "lists", "tasks", "spaces"}:
            raise UnsupportedResourceError(table, "ClickUp")

        return clickup_source(
            api_token=api_token[0], start_date=start_date, end_date=end_date
        ).with_resources(table)


class AppLovinSource:
    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        if kwargs.get("incremental_key") is not None:
            raise ValueError(
                "Applovin takes care of incrementality on its own, you should not provide incremental_key"
            )

        parsed_uri = urlparse(uri)
        params = parse_qs(parsed_uri.query)

        api_key = params.get("api_key", None)
        if api_key is None:
            raise MissingValueError("api_key", "AppLovin")

        interval_start = kwargs.get("interval_start")
        interval_end = kwargs.get("interval_end")

        now = datetime.now()
        start_date = (
            interval_start if interval_start is not None else now - timedelta(days=1)
        )
        end_date = interval_end

        custom_report = None
        if table.startswith("custom:"):
            custom_report = table
            table = "custom_report"

        from ingestr.src.applovin import applovin_source

        src = applovin_source(
            api_key[0],
            start_date.strftime("%Y-%m-%d"),
            end_date.strftime("%Y-%m-%d") if end_date else None,
            custom_report,
        )

        if table not in src.resources:
            raise UnsupportedResourceError(table, "AppLovin")

        return src.with_resources(table)


class ApplovinMaxSource:
    # expected uri format: applovinmax://?api_key=<api_key>
    # expected table format: user_ad_revenue:app_id_1,app_id_2

    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        if kwargs.get("incremental_key"):
            raise ValueError(
                "AppLovin Max takes care of incrementality on its own, you should not provide incremental_key"
            )

        parsed_uri = urlparse(uri)
        params = parse_qs(parsed_uri.query)

        api_key = params.get("api_key")
        if api_key is None:
            raise ValueError("api_key is required to connect to AppLovin Max API.")

        AVAILABLE_TABLES = ["user_ad_revenue"]

        table_fields = table.split(":")
        requested_table = table_fields[0]

        if len(table_fields) != 2:
            raise ValueError(
                "Invalid table format. Expected format is user_ad_revenue:app_id_1,app_id_2"
            )

        if requested_table not in AVAILABLE_TABLES:
            raise ValueError(
                f"Table name '{requested_table}' is not supported for AppLovin Max source yet."
                f"Only '{AVAILABLE_TABLES}' are currently supported. "
                "If you need additional tables, please create a GitHub issue at "
                "https://github.com/bruin-data/ingestr"
            )

        applications = [
            i for i in table_fields[1].replace(" ", "").split(",") if i.strip()
        ]
        if len(applications) == 0:
            raise ValueError("At least one application id is required")

        if len(applications) != len(set(applications)):
            raise ValueError("Application ids must be unique.")

        interval_start = kwargs.get("interval_start")
        interval_end = kwargs.get("interval_end")

        now = pendulum.now("UTC")
        default_start = now.subtract(days=30).date()

        start_date = (
            interval_start.date() if interval_start is not None else default_start
        )

        end_date = interval_end.date() if interval_end is not None else None

        from ingestr.src.applovin_max import applovin_max_source

        return applovin_max_source(
            start_date=start_date,
            end_date=end_date,
            api_key=api_key[0],
            applications=applications,
        ).with_resources(requested_table)


class SalesforceSource:
    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        if kwargs.get("incremental_key"):
            raise ValueError(
                "Salesforce takes care of incrementality on its own, you should not provide incremental_key"
            )

        params = parse_qs(urlparse(uri).query)
        creds = {
            "username": params.get("username", [None])[0],
            "password": params.get("password", [None])[0],
            "token": params.get("token", [None])[0],
            "domain": params.get("domain", [None])[0],
        }
        for k, v in creds.items():
            if v is None:
                raise MissingValueError(k, "Salesforce")

        from ingestr.src.salesforce import salesforce_source

        src = salesforce_source(**creds)  # type: ignore

        if table.startswith("custom:"):
            custom_object = table.split(":")[1]
            src = salesforce_source(**creds, custom_object=custom_object)
            return src.with_resources("custom")

        if table not in src.resources:
            raise UnsupportedResourceError(table, "Salesforce")

        return src.with_resources(table)


class PersonioSource:
    def handles_incrementality(self) -> bool:
        return True

    # applovin://?client_id=123&client_secret=123
    def dlt_source(self, uri: str, table: str, **kwargs):
        if kwargs.get("incremental_key"):
            raise ValueError(
                "Personio takes care of incrementality on its own, you should not provide incremental_key"
            )

        parsed_uri = urlparse(uri)
        params = parse_qs(parsed_uri.query)

        client_id = params.get("client_id")
        client_secret = params.get("client_secret")

        interval_start = kwargs.get("interval_start")
        interval_end = kwargs.get("interval_end")

        interval_start_date = (
            interval_start if interval_start is not None else "2018-01-01"
        )

        interval_end_date = (
            interval_end.strftime("%Y-%m-%d") if interval_end is not None else None
        )

        if client_id is None:
            raise MissingValueError("client_id", "Personio")
        if client_secret is None:
            raise MissingValueError("client_secret", "Personio")
        if table not in [
            "employees",
            "absences",
            "absence_types",
            "attendances",
            "projects",
            "document_categories",
            "employees_absences_balance",
            "custom_reports_list",
        ]:
            raise UnsupportedResourceError(table, "Personio")

        from ingestr.src.personio import personio_source

        return personio_source(
            client_id=client_id[0],
            client_secret=client_secret[0],
            start_date=interval_start_date,
            end_date=interval_end_date,
        ).with_resources(table)


class KinesisSource:
    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        # kinesis://?aws_access_key_id=<AccessKeyId>&aws_secret_access_key=<SecretAccessKey>&region_name=<Region>
        # source table = stream name
        parsed_uri = urlparse(uri)
        params = parse_qs(parsed_uri.query)

        aws_access_key_id = params.get("aws_access_key_id")
        if aws_access_key_id is None:
            raise MissingValueError("aws_access_key_id", "Kinesis")

        aws_secret_access_key = params.get("aws_secret_access_key")
        if aws_secret_access_key is None:
            raise MissingValueError("aws_secret_access_key", "Kinesis")

        region_name = params.get("region_name")
        if region_name is None:
            raise MissingValueError("region_name", "Kinesis")

        start_date = kwargs.get("interval_start")
        if start_date is not None:
            # the resource will read all messages after this timestamp.
            start_date = ensure_pendulum_datetime(start_date)

        from dlt.common.configuration.specs import AwsCredentials

        from ingestr.src.kinesis import kinesis_stream

        credentials = AwsCredentials(
            aws_access_key_id=aws_access_key_id[0],
            aws_secret_access_key=aws_secret_access_key[0],
            region_name=region_name[0],
        )

        return kinesis_stream(
            stream_name=table, credentials=credentials, initial_at_timestamp=start_date
        )


class PipedriveSource:
    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        if kwargs.get("incremental_key"):
            raise ValueError(
                "Pipedrive takes care of incrementality on its own, you should not provide incremental_key"
            )

        parsed_uri = urlparse(uri)
        params = parse_qs(parsed_uri.query)
        api_key = params.get("api_token")
        if api_key is None:
            raise MissingValueError("api_token", "Pipedrive")

        start_date = kwargs.get("interval_start")
        if start_date is not None:
            start_date = ensure_pendulum_datetime(start_date)
        else:
            start_date = pendulum.parse("2000-01-01")

        if table not in [
            "users",
            "activities",
            "persons",
            "organizations",
            "products",
            "stages",
            "deals",
        ]:
            raise UnsupportedResourceError(table, "Pipedrive")

        from ingestr.src.pipedrive import pipedrive_source

        return pipedrive_source(
            pipedrive_api_key=api_key, since_timestamp=start_date
        ).with_resources(table)


class FrankfurterSource:
    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        if kwargs.get("incremental_key"):
            raise ValueError(
                "Frankfurter takes care of incrementality on its own, you should not provide incremental_key"
            )

        from ingestr.src.frankfurter import frankfurter_source
        from ingestr.src.frankfurter.helpers import validate_currency, validate_dates

        parsed_uri = urlparse(uri)
        source_params = parse_qs(parsed_uri.query)
        base_currency = source_params.get("base", [None])[0]

        if not base_currency:
            base_currency = "USD"

        validate_currency(base_currency)

        if kwargs.get("interval_start"):
            start_date = ensure_pendulum_datetime(str(kwargs.get("interval_start")))
        else:
            start_date = pendulum.yesterday()

        if kwargs.get("interval_end"):
            end_date = ensure_pendulum_datetime(str(kwargs.get("interval_end")))
        else:
            end_date = None

        validate_dates(start_date=start_date, end_date=end_date)

        src = frankfurter_source(
            start_date=start_date,
            end_date=end_date,
            base_currency=base_currency,
        )

        if table not in src.resources:
            raise UnsupportedResourceError(table, "Frankfurter")

        return src.with_resources(table)


class FreshdeskSource:
    # freshdesk://domain?api_key=<api_key>
    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        if kwargs.get("incremental_key"):
            raise ValueError(
                "Freshdesk takes care of incrementality on its own, you should not provide incremental_key"
            )

        parsed_uri = urlparse(uri)
        domain = parsed_uri.netloc
        query = parsed_uri.query
        params = parse_qs(query)

        if not domain:
            raise MissingValueError("domain", "Freshdesk")

        if "." in domain:
            domain = domain.split(".")[0]

        api_key = params.get("api_key")
        if api_key is None:
            raise MissingValueError("api_key", "Freshdesk")

        start_date = kwargs.get("interval_start")
        if start_date is not None:
            start_date = ensure_pendulum_datetime(start_date).in_tz("UTC")
        else:
            start_date = ensure_pendulum_datetime("2022-01-01T00:00:00Z")

        end_date = kwargs.get("interval_end")
        if end_date is not None:
            end_date = ensure_pendulum_datetime(end_date).in_tz("UTC")
        else:
            end_date = None

        custom_query: Optional[str] = None
        if ":" in table:
            table, custom_query = table.split(":", 1)

        if table not in [
            "agents",
            "companies",
            "contacts",
            "groups",
            "roles",
            "tickets",
        ]:
            raise UnsupportedResourceError(table, "Freshdesk")

        if custom_query and table != "tickets":
            raise ValueError(f"Custom query is not supported for {table}")

        from ingestr.src.freshdesk import freshdesk_source

        return freshdesk_source(
            api_secret_key=api_key[0],
            domain=domain,
            start_date=start_date,
            end_date=end_date,
            query=custom_query,
        ).with_resources(table)


class TrustpilotSource:
    # trustpilot://<business_unit_id>?api_key=<api_key>
    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        if kwargs.get("incremental_key"):
            raise ValueError(
                "Trustpilot takes care of incrementality on its own, you should not provide incremental_key"
            )

        parsed_uri = urlparse(uri)
        business_unit_id = parsed_uri.netloc
        params = parse_qs(parsed_uri.query)

        if not business_unit_id:
            raise MissingValueError("business_unit_id", "Trustpilot")

        api_key = params.get("api_key")
        if api_key is None:
            raise MissingValueError("api_key", "Trustpilot")

        start_date = kwargs.get("interval_start")
        if start_date is None:
            start_date = ensure_pendulum_datetime("2000-01-01").in_tz("UTC").isoformat()
        else:
            start_date = ensure_pendulum_datetime(start_date).in_tz("UTC").isoformat()

        end_date = kwargs.get("interval_end")

        if end_date is not None:
            end_date = ensure_pendulum_datetime(end_date).in_tz("UTC").isoformat()

        if table not in ["reviews"]:
            raise UnsupportedResourceError(table, "Trustpilot")

        from ingestr.src.trustpilot import trustpilot_source

        return trustpilot_source(
            business_unit_id=business_unit_id,
            api_key=api_key[0],
            start_date=start_date,
            end_date=end_date,
        ).with_resources(table)


class PhantombusterSource:
    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        if kwargs.get("incremental_key"):
            raise ValueError(
                "Phantombuster takes care of incrementality on its own, you should not provide incremental_key"
            )

        # phantombuster://?api_key=<api_key>
        # source table = phantom_results:agent_id
        parsed_uri = urlparse(uri)
        params = parse_qs(parsed_uri.query)
        api_key = params.get("api_key")
        if api_key is None:
            raise MissingValueError("api_key", "Phantombuster")

        table_fields = table.replace(" ", "").split(":")
        table_name = table_fields[0]

        agent_id = table_fields[1] if len(table_fields) > 1 else None

        if table_name not in ["completed_phantoms"]:
            raise UnsupportedResourceError(table_name, "Phantombuster")

        if not agent_id:
            raise MissingValueError("agent_id", "Phantombuster")

        start_date = kwargs.get("interval_start")
        if start_date is None:
            start_date = ensure_pendulum_datetime("2018-01-01").in_tz("UTC")
        else:
            start_date = ensure_pendulum_datetime(start_date).in_tz("UTC")

        end_date = kwargs.get("interval_end")
        if end_date is not None:
            end_date = ensure_pendulum_datetime(end_date).in_tz("UTC")

        from ingestr.src.phantombuster import phantombuster_source

        return phantombuster_source(
            api_key=api_key[0],
            agent_id=agent_id,
            start_date=start_date,
            end_date=end_date,
        ).with_resources(table_name)


class ElasticsearchSource:
    def handles_incrementality(self) -> bool:
        return False

    def dlt_source(self, uri: str, table: str, **kwargs):
        from ingestr.src.elasticsearch import elasticsearch_source

        incremental = None
        if kwargs.get("incremental_key"):
            start_value = kwargs.get("interval_start")
            end_value = kwargs.get("interval_end")

            incremental = dlt_incremental(
                kwargs.get("incremental_key", ""),
                initial_value=start_value,
                end_value=end_value,
                range_end="closed",
                range_start="closed",
            )

        # elasticsearch://localhost:9200?secure=true&verify_certs=false
        parsed = urlparse(uri)

        index = table
        if not index:
            raise ValueError(
                "Table name must be provided which is the index name in elasticsearch"
            )

        query_params = parsed.query
        params = parse_qs(query_params)

        secure = True
        if "secure" in params:
            secure = params["secure"][0].capitalize() == "True"

        verify_certs = True
        if "verify_certs" in params:
            verify_certs = params["verify_certs"][0].capitalize() == "True"

        scheme = "https" if secure else "http"
        netloc = parsed.netloc
        connection_url = f"{scheme}://{netloc}"

        return elasticsearch_source(
            connection_url=connection_url,
            index=index,
            verify_certs=verify_certs,
            incremental=incremental,
        ).with_resources(table)


class AttioSource:
    def handles_incrementality(self) -> bool:
        return False

    def dlt_source(self, uri: str, table: str, **kwargs):
        parsed_uri = urlparse(uri)
        query_params = parse_qs(parsed_uri.query)
        api_key = query_params.get("api_key")

        if api_key is None:
            raise MissingValueError("api_key", "Attio")

        parts = table.replace(" ", "").split(":")
        table_name = parts[0]
        params = parts[1:]

        from ingestr.src.attio import attio_source

        try:
            return attio_source(api_key=api_key[0], params=params).with_resources(
                table_name
            )
        except ResourcesNotFoundError:
            raise UnsupportedResourceError(table_name, "Attio")


class SmartsheetSource:
    def handles_incrementality(self) -> bool:
        return False

    # smartsheet://?access_token=<access_token>
    def dlt_source(self, uri: str, table: str, **kwargs):
        if kwargs.get("incremental_key"):
            raise ValueError("Incremental loads are not supported for Smartsheet")

        if not table:
            raise ValueError(
                "Source table (sheet_id) is required to connect to Smartsheet"
            )

        source_parts = urlparse(uri)
        source_fields = parse_qs(source_parts.query)
        access_token = source_fields.get("access_token")

        if not access_token:
            raise ValueError(
                "access_token in the URI is required to connect to Smartsheet"
            )

        from ingestr.src.smartsheets import smartsheet_source

        return smartsheet_source(
            access_token=access_token[0],
            sheet_id=table,  # table is now a single sheet_id
        )


class SolidgateSource:
    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        if kwargs.get("incremental_key"):
            raise ValueError(
                "Solidgate takes care of incrementality on its own, you should not provide incremental_key"
            )

        parsed_uri = urlparse(uri)
        query_params = parse_qs(parsed_uri.query)
        public_key = query_params.get("public_key")
        secret_key = query_params.get("secret_key")

        if public_key is None:
            raise MissingValueError("public_key", "Solidgate")

        if secret_key is None:
            raise MissingValueError("secret_key", "Solidgate")

        table_name = table.replace(" ", "")

        start_date = kwargs.get("interval_start")
        if start_date is None:
            start_date = pendulum.yesterday().in_tz("UTC")
        else:
            start_date = ensure_pendulum_datetime(start_date).in_tz("UTC")

        end_date = kwargs.get("interval_end")

        if end_date is not None:
            end_date = ensure_pendulum_datetime(end_date).in_tz("UTC")

        from ingestr.src.solidgate import solidgate_source

        try:
            return solidgate_source(
                public_key=public_key[0],
                secret_key=secret_key[0],
                start_date=start_date,
                end_date=end_date,
            ).with_resources(table_name)
        except ResourcesNotFoundError:
            raise UnsupportedResourceError(table_name, "Solidgate")


class SFTPSource:
    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        parsed_uri = urlparse(uri)
        host = parsed_uri.hostname
        if not host:
            raise MissingValueError("host", "SFTP URI")
        port = parsed_uri.port or 22
        username = parsed_uri.username
        password = parsed_uri.password

        params: Dict[str, Any] = {
            "host": host,
            "port": port,
            "username": username,
            "password": password,
            "look_for_keys": False,
            "allow_agent": False,
        }

        try:
            fs = fsspec.filesystem("sftp", **params)
        except Exception as e:
            raise ConnectionError(
                f"Failed to connect or authenticate to sftp server {host}:{port}. Error: {e}"
            )
        bucket_url = f"sftp://{host}:{port}"

        if table.startswith("/"):
            file_glob = table
        else:
            file_glob = f"/{table}"

        try:
            endpoint = blob.parse_endpoint(table)
        except blob.UnsupportedEndpointError:
            raise ValueError(
                "SFTP Source only supports specific formats files: csv, jsonl, parquet"
            )
        except Exception as e:
            raise ValueError(f"Failed to parse endpoint from path: {table}") from e

        from ingestr.src.filesystem import readers

        dlt_source_resource = readers(bucket_url, fs, file_glob)
        return dlt_source_resource.with_resources(endpoint)


class QuickBooksSource:
    def handles_incrementality(self) -> bool:
        return True

    # quickbooks://?company_id=<company_id>&client_id=<client_id>&client_secret=<client_secret>&refresh_token=<refresh>&access_token=<access_token>&environment=<env>&minor_version=<version>
    def dlt_source(self, uri: str, table: str, **kwargs):
        if kwargs.get("incremental_key"):
            raise ValueError(
                "QuickBooks takes care of incrementality on its own, you should not provide incremental_key"
            )

        parsed_uri = urlparse(uri)

        params = parse_qs(parsed_uri.query)
        company_id = params.get("company_id")
        client_id = params.get("client_id")
        client_secret = params.get("client_secret")
        refresh_token = params.get("refresh_token")
        environment = params.get("environment", ["production"])
        minor_version = params.get("minor_version", [None])

        if not client_id or not client_id[0].strip():
            raise MissingValueError("client_id", "QuickBooks")

        if not client_secret or not client_secret[0].strip():
            raise MissingValueError("client_secret", "QuickBooks")

        if not refresh_token or not refresh_token[0].strip():
            raise MissingValueError("refresh_token", "QuickBooks")

        if not company_id or not company_id[0].strip():
            raise MissingValueError("company_id", "QuickBooks")

        if environment[0] not in ["production", "sandbox"]:
            raise ValueError(
                "Invalid environment. Must be either 'production' or 'sandbox'."
            )

        from ingestr.src.quickbooks import quickbooks_source

        table_name = table.replace(" ", "")
        table_mapping = {
            "customers": "customer",
            "invoices": "invoice",
            "accounts": "account",
            "vendors": "vendor",
            "payments": "payment",
        }
        if table_name in table_mapping:
            table_name = table_mapping[table_name]

        start_date = kwargs.get("interval_start")
        if start_date is None:
            start_date = ensure_pendulum_datetime("2025-01-01").in_tz("UTC")
        else:
            start_date = ensure_pendulum_datetime(start_date).in_tz("UTC")

        end_date = kwargs.get("interval_end")

        if end_date is not None:
            end_date = ensure_pendulum_datetime(end_date).in_tz("UTC")

        return quickbooks_source(
            company_id=company_id[0],
            start_date=start_date,
            end_date=end_date,
            client_id=client_id[0],
            client_secret=client_secret[0],
            refresh_token=refresh_token[0],
            environment=environment[0],
            minor_version=minor_version[0],
            object=table_name,
        ).with_resources(table_name)


class IsocPulseSource:
    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        if kwargs.get("incremental_key"):
            raise ValueError(
                "Internet Society Pulse takes care of incrementality on its own, you should not provide incremental_key"
            )

        parsed_uri = urlparse(uri)
        params = parse_qs(parsed_uri.query)
        token = params.get("token")
        if not token or not token[0].strip():
            raise MissingValueError("token", "Internet Society Pulse")

        start_date = kwargs.get("interval_start")
        if start_date is None:
            start_date = pendulum.now().in_tz("UTC").subtract(days=30)

        end_date = kwargs.get("interval_end")

        metric = table
        opts = []
        if ":" in metric:
            metric, *opts = metric.strip().split(":")
            opts = [opt.strip() for opt in opts]

        from ingestr.src.isoc_pulse import pulse_source

        src = pulse_source(
            token=token[0],
            start_date=start_date.strftime("%Y-%m-%d"),
            end_date=end_date.strftime("%Y-%m-%d") if end_date else None,
            metric=metric,
            opts=opts,
        )
        return src.with_resources(metric)


class PinterestSource:
    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        if kwargs.get("incremental_key"):
            raise ValueError(
                "Pinterest takes care of incrementality on its own, you should not provide incremental_key"
            )

        parsed = urlparse(uri)
        params = parse_qs(parsed.query)
        access_token = params.get("access_token")

        if not access_token:
            raise MissingValueError("access_token", "Pinterest")

        start_date = kwargs.get("interval_start")
        if start_date is not None:
            start_date = ensure_pendulum_datetime(start_date)
        else:
            start_date = pendulum.datetime(2020, 1, 1).in_tz("UTC")

        end_date = kwargs.get("interval_end")
        if end_date is not None:
            end_date = end_date = ensure_pendulum_datetime(end_date).in_tz("UTC")

        from ingestr.src.pinterest import pinterest_source

        if table not in {"pins", "boards"}:
            raise UnsupportedResourceError(table, "Pinterest")

        return pinterest_source(
            access_token=access_token[0],
            start_date=start_date,
            end_date=end_date,
        ).with_resources(table)


class FluxxSource:
    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        if kwargs.get("incremental_key"):
            raise ValueError(
                "Fluxx takes care of incrementality on its own, you should not provide incremental_key"
            )

        # Parse URI: fluxx://instance?client_id=xxx&client_secret=xxx
        parsed_uri = urlparse(uri)
        source_params = parse_qs(parsed_uri.query)

        instance = parsed_uri.hostname
        if not instance:
            raise ValueError(
                "Instance is required in the URI (e.g., fluxx://mycompany.preprod)"
            )

        client_id = source_params.get("client_id")
        if not client_id:
            raise ValueError("client_id in the URI is required to connect to Fluxx")

        client_secret = source_params.get("client_secret")
        if not client_secret:
            raise ValueError("client_secret in the URI is required to connect to Fluxx")

        # Parse date parameters
        start_date = kwargs.get("interval_start")
        if start_date:
            start_date = ensure_pendulum_datetime(start_date)

        end_date = kwargs.get("interval_end")
        if end_date:
            end_date = ensure_pendulum_datetime(end_date)

        # Import Fluxx source
        from ingestr.src.fluxx import fluxx_source

        # Parse table specification for custom column selection
        # Format: "resource_name:field1,field2,field3" or "resource_name"
        resources = None
        custom_fields = {}

        if table:
            # Handle single resource with custom fields or multiple resources
            if ":" in table and table.count(":") == 1:
                # Single resource with custom fields: "grant_request:id,name,amount"
                resource_name, field_list = table.split(":", 1)
                resource_name = resource_name.strip()
                fields = [f.strip() for f in field_list.split(",")]
                resources = [resource_name]
                custom_fields[resource_name] = fields
            else:
                # Multiple resources or single resource without custom fields
                # Support comma-separated list: "grant_request,user"
                resources = [r.strip() for r in table.split(",")]

        return fluxx_source(
            instance=instance,
            client_id=client_id[0],
            client_secret=client_secret[0],
            start_date=start_date,
            end_date=end_date,
            resources=resources,
            custom_fields=custom_fields,
        )


class LinearSource:
    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        if kwargs.get("incremental_key"):
            raise ValueError(
                "Linear takes care of incrementality on its own, you should not provide incremental_key"
            )

        parsed_uri = urlparse(uri)
        params = parse_qs(parsed_uri.query)
        api_key = params.get("api_key")
        if api_key is None:
            raise MissingValueError("api_key", "Linear")

        if table not in [
            "issues",
            "projects",
            "teams",
            "users",
            "workflow_states",
            "cycles",
            "attachments",
            "comments",
            "documents",
            "external_users",
            "initiative",
            "integrations",
            "labels",
            "organization",
            "project_updates",
            "team_memberships",
            "initiative_to_project",
            "project_milestone",
            "project_status",
        ]:
            raise UnsupportedResourceError(table, "Linear")

        start_date = kwargs.get("interval_start")
        if start_date is not None:
            start_date = ensure_pendulum_datetime(start_date)
        else:
            start_date = pendulum.datetime(2020, 1, 1).in_tz("UTC")

        end_date = kwargs.get("interval_end")
        if end_date is not None:
            end_date = end_date = ensure_pendulum_datetime(end_date).in_tz("UTC")

        from ingestr.src.linear import linear_source

        return linear_source(
            api_key=api_key[0],
            start_date=start_date,
            end_date=end_date,
        ).with_resources(table)


class RevenueCatSource:
    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        if kwargs.get("incremental_key"):
            raise ValueError(
                "RevenueCat takes care of incrementality on its own, you should not provide incremental_key"
            )

        parsed_uri = urlparse(uri)
        params = parse_qs(parsed_uri.query)

        api_key = params.get("api_key")
        if api_key is None:
            raise MissingValueError("api_key", "RevenueCat")

        project_id = params.get("project_id")
        if project_id is None and table != "projects":
            raise MissingValueError("project_id", "RevenueCat")

        if table not in [
            "customers",
            "products",
            "entitlements",
            "offerings",
            "subscriptions",
            "purchases",
            "projects",
        ]:
            raise UnsupportedResourceError(table, "RevenueCat")

        start_date = kwargs.get("interval_start")
        if start_date is not None:
            start_date = ensure_pendulum_datetime(start_date)
        else:
            start_date = pendulum.datetime(2020, 1, 1).in_tz("UTC")

        end_date = kwargs.get("interval_end")
        if end_date is not None:
            end_date = ensure_pendulum_datetime(end_date).in_tz("UTC")

        from ingestr.src.revenuecat import revenuecat_source

        return revenuecat_source(
            api_key=api_key[0],
            project_id=project_id[0] if project_id is not None else None,
        ).with_resources(table)


class ZoomSource:
    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        if kwargs.get("incremental_key"):
            raise ValueError(
                "Zoom takes care of incrementality on its own, you should not provide incremental_key"
            )

        parsed = urlparse(uri)
        params = parse_qs(parsed.query)
        client_id = params.get("client_id")
        client_secret = params.get("client_secret")
        account_id = params.get("account_id")

        if not (client_id and client_secret and account_id):
            raise MissingValueError(
                "client_id/client_secret/account_id",
                "Zoom",
            )

        start_date = kwargs.get("interval_start")
        if start_date is not None:
            start_date = ensure_pendulum_datetime(start_date)
        else:
            start_date = pendulum.datetime(2020, 1, 26).in_tz("UTC")

        end_date = kwargs.get("interval_end")
        if end_date is not None:
            end_date = end_date = ensure_pendulum_datetime(end_date).in_tz("UTC")

        from ingestr.src.zoom import zoom_source

        if table not in {"meetings", "users", "participants"}:
            raise UnsupportedResourceError(table, "Zoom")

        return zoom_source(
            client_id=client_id[0],
            client_secret=client_secret[0],
            account_id=account_id[0],
            start_date=start_date,
            end_date=end_date,
        ).with_resources(table)


class InfluxDBSource:
    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        if kwargs.get("incremental_key"):
            raise ValueError(
                "InfluxDB takes care of incrementality on its own, you should not provide incremental_key"
            )

        parsed_uri = urlparse(uri)
        params = parse_qs(parsed_uri.query)
        host = parsed_uri.hostname
        port = parsed_uri.port

        secure = params.get("secure", ["true"])[0].lower() != "false"
        scheme = "https" if secure else "http"

        if port:
            host_url = f"{scheme}://{host}:{port}"
        else:
            host_url = f"{scheme}://{host}"

        token = params.get("token")
        org = params.get("org")
        bucket = params.get("bucket")

        if not host:
            raise MissingValueError("host", "InfluxDB")
        if not token:
            raise MissingValueError("token", "InfluxDB")
        if not org:
            raise MissingValueError("org", "InfluxDB")
        if not bucket:
            raise MissingValueError("bucket", "InfluxDB")

        start_date = kwargs.get("interval_start")
        if start_date is not None:
            start_date = ensure_pendulum_datetime(start_date)
        else:
            start_date = pendulum.datetime(2024, 1, 1).in_tz("UTC")

        end_date = kwargs.get("interval_end")
        if end_date is not None:
            end_date = ensure_pendulum_datetime(end_date)

        from ingestr.src.influxdb import influxdb_source

        return influxdb_source(
            measurement=table,
            host=host_url,
            org=org[0],
            bucket=bucket[0],
            token=token[0],
            secure=secure,
            start_date=start_date,
            end_date=end_date,
        ).with_resources(table)


class WiseSource:
    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        parsed = urlparse(uri)
        params = parse_qs(parsed.query)
        api_key = params.get("api_key")

        if not api_key:
            raise MissingValueError("api_key", "Wise")

        if table not in ["profiles", "transfers", "balances"]:
            raise ValueError(
                f"Resource '{table}' is not supported for Wise source yet, if you are interested in it please create a GitHub issue at https://github.com/bruin-data/ingestr"
            )

        start_date = kwargs.get("interval_start")
        if start_date:
            start_date = ensure_pendulum_datetime(start_date).in_timezone("UTC")
        else:
            start_date = pendulum.datetime(2020, 1, 1).in_timezone("UTC")

        end_date = kwargs.get("interval_end")
        if end_date:
            end_date = ensure_pendulum_datetime(end_date).in_timezone("UTC")
        else:
            end_date = None

        from ingestr.src.wise import wise_source

        return wise_source(
            api_key=api_key[0],
            start_date=start_date,
            end_date=end_date,
        ).with_resources(table)


class FundraiseupSource:
    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        parsed_uri = urlparse(uri)
        params = parse_qs(parsed_uri.query)

        api_key = params.get("api_key")
        if api_key is None:
            raise MissingValueError("api_key", "Fundraiseup")

        from ingestr.src.fundraiseup import fundraiseup_source

        src = fundraiseup_source(api_key=api_key[0])
        if table not in src.resources:
            raise UnsupportedResourceError(table, "Fundraiseup")
        return src.with_resources(table)


class AnthropicSource:
    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        # anthropic://?api_key=<admin_api_key>
        parsed_uri = urlparse(uri)
        params = parse_qs(parsed_uri.query)

        api_key = params.get("api_key")
        if api_key is None:
            raise MissingValueError("api_key", "Anthropic")

        if table not in [
            "claude_code_usage",
            "usage_report",
            "cost_report",
            "organization",
            "workspaces",
            "api_keys",
            "invites",
            "users",
            "workspace_members",
        ]:
            raise UnsupportedResourceError(table, "Anthropic")

        # Get start and end dates from kwargs
        start_date = kwargs.get("interval_start")
        if start_date:
            start_date = ensure_pendulum_datetime(start_date)
        else:
            # Default to 2023-01-01
            start_date = pendulum.datetime(2023, 1, 1)

        end_date = kwargs.get("interval_end")
        if end_date:
            end_date = ensure_pendulum_datetime(end_date)
        else:
            end_date = None

        from ingestr.src.anthropic import anthropic_source

        return anthropic_source(
            api_key=api_key[0],
            initial_start_date=start_date,
            end_date=end_date,
        ).with_resources(table)


class PlusVibeAISource:
    resources = [
        "campaigns",
        "leads",
        "email_accounts",
        "emails",
        "blocklist",
        "webhooks",
        "tags",
    ]

    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        # plusvibeai://?api_key=<key>&workspace_id=<id>
        parsed_uri = urlparse(uri)
        params = parse_qs(parsed_uri.query)

        api_key = params.get("api_key")
        workspace_id = params.get("workspace_id")

        if not api_key:
            raise MissingValueError("api_key", "PlusVibeAI")

        if not workspace_id:
            raise MissingValueError("workspace_id", "PlusVibeAI")

        if table not in self.resources:
            raise UnsupportedResourceError(table, "PlusVibeAI")

        import dlt

        from ingestr.src.plusvibeai import plusvibeai_source

        dlt.secrets["sources.plusvibeai.api_key"] = api_key[0]
        dlt.secrets["sources.plusvibeai.workspace_id"] = workspace_id[0]

        # Handle custom base URL if provided
        base_url = params.get("base_url", ["https://api.plusvibe.ai"])[0]
        dlt.secrets["sources.plusvibeai.base_url"] = base_url

        src = plusvibeai_source()
        return src.with_resources(table)


class IntercomSource:
    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        # intercom://?access_token=<token>&region=<us|eu|au>
        # OR intercom://?oauth_token=<token>&region=<us|eu|au>
        parsed_uri = urlparse(uri)
        params = parse_qs(parsed_uri.query)

        # Check for authentication
        access_token = params.get("access_token")
        oauth_token = params.get("oauth_token")
        region = params.get("region", ["us"])[0]

        if not access_token and not oauth_token:
            raise MissingValueError("access_token or oauth_token", "Intercom")

        # Validate table/resource
        supported_tables = [
            "contacts",
            "companies",
            "conversations",
            "tickets",
            "tags",
            "segments",
            "teams",
            "admins",
            "articles",
            "data_attributes",
        ]

        if table not in supported_tables:
            raise UnsupportedResourceError(table, "Intercom")

        # Get date parameters
        start_date = kwargs.get("interval_start")
        if start_date:
            start_date = ensure_pendulum_datetime(start_date)
        else:
            start_date = pendulum.datetime(2020, 1, 1)

        end_date = kwargs.get("interval_end")
        if end_date:
            end_date = ensure_pendulum_datetime(end_date)

        # Import and initialize the source
        from ingestr.src.intercom import (
            IntercomCredentialsAccessToken,
            IntercomCredentialsOAuth,
            TIntercomCredentials,
            intercom_source,
        )

        credentials: TIntercomCredentials
        if access_token:
            credentials = IntercomCredentialsAccessToken(
                access_token=access_token[0], region=region
            )
        else:
            if not oauth_token:
                raise MissingValueError("oauth_token", "Intercom")
            credentials = IntercomCredentialsOAuth(
                oauth_token=oauth_token[0], region=region
            )

        return intercom_source(
            credentials=credentials,
            start_date=start_date,
            end_date=end_date,
        ).with_resources(table)


class HttpSource:
    """Source for reading CSV, JSON, and Parquet files from HTTP URLs"""

    def handles_incrementality(self) -> bool:
        return False

    def dlt_source(self, uri: str, table: str, **kwargs):
        """
        Create a dlt source for reading files from HTTP URLs.

        URI format: http://example.com/file.csv or https://example.com/file.json

        Args:
            uri: HTTP(S) URL to the file
            table: Not used for HTTP source (files are read directly)
            **kwargs: Additional arguments:
                - file_format: Optional file format override ('csv', 'json', 'parquet')
                - chunksize: Number of records to process at once (default varies by format)
                - merge_key: Merge key for the resource

        Returns:
            DltResource for the HTTP file
        """
        from ingestr.src.http import http_source

        # Extract the actual URL (remove the http:// or https:// scheme if duplicated)
        url = uri
        if uri.startswith("http://http://") or uri.startswith("https://https://"):
            url = uri.split("://", 1)[1]

        file_format = kwargs.get("file_format")
        chunksize = kwargs.get("chunksize")
        merge_key = kwargs.get("merge_key")

        reader_kwargs = {}
        if chunksize is not None:
            reader_kwargs["chunksize"] = chunksize

        source = http_source(url=url, file_format=file_format, **reader_kwargs)

        if merge_key:
            source.apply_hints(merge_key=merge_key)

        return source


class MondaySource:
    def handles_incrementality(self) -> bool:
        return False

    def dlt_source(self, uri: str, table: str, **kwargs):
        parsed_uri = urlparse(uri)
        query_params = parse_qs(parsed_uri.query)
        api_token = query_params.get("api_token")

        if api_token is None:
            raise MissingValueError("api_token", "Monday")

        parts = table.replace(" ", "").split(":")
        table_name = parts[0]
        params = parts[1:]

        # Get interval_start and interval_end from kwargs (command line args)
        interval_start = kwargs.get("interval_start")
        interval_end = kwargs.get("interval_end")

        # Convert datetime to string format YYYY-MM-DD
        start_date = interval_start.strftime("%Y-%m-%d") if interval_start else None
        end_date = interval_end.strftime("%Y-%m-%d") if interval_end else None

        from ingestr.src.monday import monday_source

        try:
            return monday_source(
                api_token=api_token[0],
                params=params,
                start_date=start_date,
                end_date=end_date,
            ).with_resources(table_name)
        except ResourcesNotFoundError:
            raise UnsupportedResourceError(table_name, "Monday")


class MailchimpSource:
    def handles_incrementality(self) -> bool:
        return False

    def dlt_source(self, uri: str, table: str, **kwargs):
        parsed_uri = urlparse(uri)
        query_params = parse_qs(parsed_uri.query)
        api_key = query_params.get("api_key")
        server = query_params.get("server")

        if api_key is None:
            raise MissingValueError("api_key", "Mailchimp")
        if server is None:
            raise MissingValueError("server", "Mailchimp")

        from ingestr.src.mailchimp import mailchimp_source

        try:
            return mailchimp_source(
                api_key=api_key[0],
                server=server[0],
            ).with_resources(table)
        except ResourcesNotFoundError:
            raise UnsupportedResourceError(table, "Mailchimp")


class AlliumSource:
    def handles_incrementality(self) -> bool:
        return False

    def dlt_source(self, uri: str, table: str, **kwargs):
        parsed_uri = urlparse(uri)
        query_params = parse_qs(parsed_uri.query)
        api_key = query_params.get("api_key")

        if api_key is None:
            raise MissingValueError("api_key", "Allium")

        # Extract query_id and custom parameters from table parameter
        # Format: query_id or query:query_id or query:query_id:param1=value1&param2=value2
        query_id = table
        custom_params = {}
        limit = None
        compute_profile = None

        if ":" in table:
            parts = table.split(":", 2)  # Split into max 3 parts
            if len(parts) >= 2:
                query_id = parts[1]
            if len(parts) == 3:
                # Parse custom parameters from query string format
                param_string = parts[2]
                for param in param_string.split("&"):
                    if "=" in param:
                        key, value = param.split("=", 1)
                        # Extract run_config parameters
                        if key == "limit":
                            limit = int(value)
                        elif key == "compute_profile":
                            compute_profile = value
                        else:
                            custom_params[key] = value

        # Extract parameters from interval_start and interval_end
        # Default: 2 days ago 00:00 to yesterday 00:00
        now = pendulum.now()
        default_start = now.subtract(days=2).start_of("day")
        default_end = now.subtract(days=1).start_of("day")

        parameters = {}
        interval_start = kwargs.get("interval_start")
        interval_end = kwargs.get("interval_end")

        start_date = interval_start if interval_start is not None else default_start
        end_date = interval_end if interval_end is not None else default_end

        parameters["start_date"] = start_date.strftime("%Y-%m-%d")
        parameters["end_date"] = end_date.strftime("%Y-%m-%d")
        parameters["start_timestamp"] = str(int(start_date.timestamp()))
        parameters["end_timestamp"] = str(int(end_date.timestamp()))

        # Merge custom parameters (they override default parameters)
        parameters.update(custom_params)

        from ingestr.src.allium import allium_source

        return allium_source(
            api_key=api_key[0],
            query_id=query_id,
            parameters=parameters if parameters else None,
            limit=limit,
            compute_profile=compute_profile,
        )


class CouchbaseSource:
    table_builder: Callable

    def __init__(self, table_builder=None) -> None:
        if table_builder is None:
            from ingestr.src.couchbase_source import couchbase_collection

            table_builder = couchbase_collection

        self.table_builder = table_builder

    def handles_incrementality(self) -> bool:
        return False

    def dlt_source(self, uri: str, table: str, **kwargs):
        """
        Create a dlt source for reading data from Couchbase.

        URI formats:
            - couchbase://username:password@host
            - couchbase://username:password@host/bucket
            - couchbase://username:password@host?ssl=true
            - couchbases://username:password@host (SSL enabled)

        Table formats:
            - bucket.scope.collection (when bucket not in URI)
            - scope.collection (when bucket specified in URI path)

        Note: If password contains special characters (@, :, /, etc.), they must be URL-encoded.

        Examples:
            Local/Self-hosted:
            - couchbase://admin:password123@localhost with table "mybucket.myscope.mycollection"
            - couchbase://admin:password123@localhost/mybucket with table "myscope.mycollection"
            - couchbase://admin:password123@localhost?ssl=true with table "mybucket._default._default"

            Capella (Cloud):
            - couchbases://user:pass@cb.xxx.cloud.couchbase.com with table "travel-sample.inventory.airport"
            - couchbase://user:pass@cb.xxx.cloud.couchbase.com/travel-sample?ssl=true with table "inventory.airport"

        To encode password in Python:
            from urllib.parse import quote
            encoded_pwd = quote("MyPass@123!", safe='')
            uri = f"couchbase://admin:{encoded_pwd}@localhost?ssl=true"

        Args:
            uri: Couchbase connection URI (can include /bucket path and ?ssl=true query parameter)
            table: Format depends on URI:
                - bucket.scope.collection (if bucket not in URI)
                - scope.collection (if bucket in URI path)
            **kwargs: Additional arguments:
                - limit: Maximum number of documents to fetch
                - incremental_key: Field to use for incremental loading
                - interval_start: Start value for incremental loading
                - interval_end: End value for incremental loading

        Returns:
            DltResource for the Couchbase collection
        """
        # Parse the URI to extract connection details
        # urlparse automatically decodes URL-encoded credentials

        parsed = urlparse(uri)

        # Extract username and password from URI
        # Note: urlparse automatically decodes URL-encoded characters in username/password
        from urllib.parse import unquote

        username = parsed.username
        password = unquote(parsed.password) if parsed.password else None

        if not username or not password:
            raise ValueError(
                "Username and password must be provided in the URI.\n"
                "Format: couchbase://username:password@host\n"
                "If password has special characters (@, :, /), URL-encode them.\n"
                "Example: couchbase://admin:MyPass%40123@localhost for password 'MyPass@123'"
            )

        # Reconstruct connection string without credentials
        scheme = parsed.scheme
        netloc = parsed.netloc

        # Remove username:password@ from netloc if present
        if "@" in netloc:
            netloc = netloc.split("@", 1)[1]

        # Parse query parameters from URI
        from urllib.parse import parse_qs

        query_params = parse_qs(parsed.query)

        # Check if SSL is requested via URI query parameter (?ssl=true)
        if "ssl" in query_params:
            ssl_value = query_params["ssl"][0].lower()
            use_ssl = ssl_value in ("true", "1", "yes")

            # Apply SSL scheme based on parameter
            if use_ssl and scheme == "couchbase":
                scheme = "couchbases"

        connection_string = f"{scheme}://{netloc}"

        # Extract bucket from URI path if present (e.g., couchbase://host/bucket)
        bucket_from_uri = None
        if parsed.path and parsed.path.strip("/"):
            bucket_from_uri = parsed.path.strip("/").split("/")[0]

        # Parse table format: can be "scope.collection" or "bucket.scope.collection"
        table_parts = table.split(".")

        if len(table_parts) == 3:
            # Format: bucket.scope.collection
            bucket, scope, collection = table_parts
        elif len(table_parts) == 2:
            # Format: scope.collection (bucket from URI)
            if bucket_from_uri:
                bucket = bucket_from_uri
                scope, collection = table_parts
            else:
                raise ValueError(
                    "Table format is 'scope.collection' but no bucket specified in URI.\n"
                    f"Either use URI format: couchbase://user:pass@host/bucket\n"
                    f"Or use table format: bucket.scope.collection\n"
                    f"Got table: {table}"
                )
        else:
            raise ValueError(
                "Table format must be 'bucket.scope.collection' or 'scope.collection' (with bucket in URI). "
                f"Got: {table}\n"
                "Examples:\n"
                "  - URI: couchbase://user:pass@host, Table: travel-sample.inventory.airport\n"
                "  - URI: couchbase://user:pass@host/travel-sample, Table: inventory.airport"
            )

        # Handle incremental loading
        incremental = None
        if kwargs.get("incremental_key"):
            start_value = kwargs.get("interval_start")
            end_value = kwargs.get("interval_end")

            incremental = dlt_incremental(
                kwargs.get("incremental_key", ""),
                initial_value=start_value,
                end_value=end_value,
                range_end="closed",
                range_start="closed",
            )

        # Get optional parameters
        limit = kwargs.get("limit")

        table_instance = self.table_builder(
            connection_string=connection_string,
            username=username,
            password=password,
            bucket=bucket,
            scope=scope,
            collection=collection,
            incremental=incremental,
            limit=limit,
        )
        table_instance.max_table_nesting = 1

        return table_instance


class CursorSource:
    resources = [
        "team_members",
        "daily_usage_data",
        "team_spend",
        "filtered_usage_events",
    ]

    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        # cursor://?api_key=<api_key>
        parsed_uri = urlparse(uri)
        params = parse_qs(parsed_uri.query)

        api_key = params.get("api_key")

        if not api_key:
            raise MissingValueError("api_key", "Cursor")

        if table not in self.resources:
            raise UnsupportedResourceError(table, "Cursor")

        import dlt

        from ingestr.src.cursor import cursor_source

        dlt.secrets["sources.cursor.api_key"] = api_key[0]

        # Handle interval_start and interval_end for daily_usage_data and filtered_usage_events (optional)
        if table in ["daily_usage_data", "filtered_usage_events"]:
            interval_start = kwargs.get("interval_start")
            interval_end = kwargs.get("interval_end")

            # Both are optional, but if one is provided, both should be provided
            if interval_start is not None and interval_end is not None:
                # Convert datetime to epoch milliseconds
                start_ms = int(interval_start.timestamp() * 1000)
                end_ms = int(interval_end.timestamp() * 1000)

                dlt.config["sources.cursor.start_date"] = start_ms
                dlt.config["sources.cursor.end_date"] = end_ms

        src = cursor_source()
        return src.with_resources(table)


class SocrataSource:
    def handles_incrementality(self) -> bool:
        return False

    def dlt_source(self, uri: str, table: str, **kwargs):
        """
        Creates a DLT source for Socrata open data platform.

        URI format: socrata://domain?app_token=TOKEN
        Table: dataset_id (e.g., "6udu-fhnu")

        Args:
            uri: Socrata connection URI with domain and optional auth params
            table: Dataset ID (e.g., "6udu-fhnu")
            **kwargs: Additional arguments:
                - incremental_key: Field to use for incremental loading (e.g., ":updated_at")
                - interval_start: Start date for initial load
                - interval_end: End date for load
                - primary_key: Primary key field for merge operations

        Returns:
            DltResource for the Socrata dataset
        """
        from urllib.parse import parse_qs, urlparse

        parsed = urlparse(uri)

        domain = parsed.netloc
        if not domain:
            raise ValueError(
                "Domain must be provided in the URI.\n"
                "Format: socrata://domain?app_token=TOKEN\n"
                "Example: socrata://evergreen.data.socrata.com?app_token=mytoken"
            )

        query_params = parse_qs(parsed.query)

        dataset_id = table
        if not dataset_id:
            raise ValueError(
                "Dataset ID must be provided as the table parameter.\n"
                "Example: --source-table 6udu-fhnu"
            )

        app_token = query_params.get("app_token", [None])[0]
        username = query_params.get("username", [None])[0]
        password = query_params.get("password", [None])[0]

        incremental = None
        if kwargs.get("incremental_key"):
            start_value = kwargs.get("interval_start")
            end_value = kwargs.get("interval_end")

            if start_value:
                start_value = (
                    start_value.isoformat()
                    if hasattr(start_value, "isoformat")
                    else str(start_value)
                )

            if end_value:
                end_value = (
                    end_value.isoformat()
                    if hasattr(end_value, "isoformat")
                    else str(end_value)
                )

            incremental = dlt_incremental(
                kwargs.get("incremental_key", ""),
                initial_value=start_value,
                end_value=end_value,
                range_end="open",
                range_start="closed",
            )

        primary_key = kwargs.get("primary_key")

        from ingestr.src.socrata_source import source

        return source(
            domain=domain,
            dataset_id=dataset_id,
            app_token=app_token,
            username=username,
            password=password,
            incremental=incremental,
            primary_key=primary_key,
        ).with_resources("dataset")


class HostawaySource:
    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        if kwargs.get("incremental_key"):
            raise ValueError(
                "Hostaway takes care of incrementality on its own, you should not provide incremental_key"
            )

        source_parts = urlparse(uri)
        source_params = parse_qs(source_parts.query)
        api_key = source_params.get("api_key")

        if not api_key:
            raise ValueError("api_key in the URI is required to connect to Hostaway")

        match table:
            case "listings":
                resource_name = "listings"
            case "listing_fee_settings":
                resource_name = "listing_fee_settings"
            case "listing_agreements":
                resource_name = "listing_agreements"
            case "listing_pricing_settings":
                resource_name = "listing_pricing_settings"
            case "cancellation_policies":
                resource_name = "cancellation_policies"
            case "cancellation_policies_airbnb":
                resource_name = "cancellation_policies_airbnb"
            case "cancellation_policies_marriott":
                resource_name = "cancellation_policies_marriott"
            case "cancellation_policies_vrbo":
                resource_name = "cancellation_policies_vrbo"
            case "reservations":
                resource_name = "reservations"
            case "finance_fields":
                resource_name = "finance_fields"
            case "reservation_payment_methods":
                resource_name = "reservation_payment_methods"
            case "reservation_rental_agreements":
                resource_name = "reservation_rental_agreements"
            case "listing_calendars":
                resource_name = "listing_calendars"
            case "conversations":
                resource_name = "conversations"
            case "message_templates":
                resource_name = "message_templates"
            case "bed_types":
                resource_name = "bed_types"
            case "property_types":
                resource_name = "property_types"
            case "countries":
                resource_name = "countries"
            case "account_tax_settings":
                resource_name = "account_tax_settings"
            case "user_groups":
                resource_name = "user_groups"
            case "guest_payment_charges":
                resource_name = "guest_payment_charges"
            case "coupons":
                resource_name = "coupons"
            case "webhook_reservations":
                resource_name = "webhook_reservations"
            case "tasks":
                resource_name = "tasks"
            case _:
                raise ValueError(
                    f"Resource '{table}' is not supported for Hostaway source yet, if you are interested in it please create a GitHub issue at https://github.com/bruin-data/ingestr"
                )

        start_date = kwargs.get("interval_start")
        if start_date:
            start_date = ensure_pendulum_datetime(start_date).in_timezone("UTC")
        else:
            start_date = pendulum.datetime(1970, 1, 1).in_timezone("UTC")

        end_date = kwargs.get("interval_end")
        if end_date:
            end_date = ensure_pendulum_datetime(end_date).in_timezone("UTC")

        from ingestr.src.hostaway import hostaway_source

        return hostaway_source(
            api_key=api_key[0],
            start_date=start_date,
            end_date=end_date,
        ).with_resources(resource_name)


class SnapchatAdsSource:
    resources = [
        "organizations",
        "fundingsources",
        "billingcenters",
        "adaccounts",
        "invoices",
        "transactions",
        "members",
        "roles",
        "campaigns",
        "adsquads",
        "ads",
        "event_details",
        "creatives",
        "segments",
        "campaigns_stats",
        "ad_accounts_stats",
        "ads_stats",
        "ad_squads_stats",
    ]

    # Resources that support ad_account_id filtering
    AD_ACCOUNT_RESOURCES = {
        "invoices",
        "campaigns",
        "adsquads",
        "ads",
        "event_details",
        "creatives",
        "segments",
    }

    # Stats resources
    STATS_RESOURCES = {
        "campaigns_stats",
        "ad_accounts_stats",
        "ads_stats",
        "ad_squads_stats",
    }

    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        parsed_uri = urlparse(uri)
        source_fields = parse_qs(parsed_uri.query)

        refresh_token = source_fields.get("refresh_token")
        if not refresh_token:
            raise ValueError("refresh_token is required to connect to Snapchat Ads")

        client_id = source_fields.get("client_id")
        if not client_id:
            raise ValueError("client_id is required to connect to Snapchat Ads")

        client_secret = source_fields.get("client_secret")
        if not client_secret:
            raise ValueError("client_secret is required to connect to Snapchat Ads")

        organization_id = source_fields.get("organization_id")

        # Parse table name
        stats_config = None
        ad_account_id = None

        if ":" in table:
            parts = table.split(":")
            resource_name = parts[0]

            if resource_name in self.STATS_RESOURCES:
                # Stats table format parsed in helpers
                from ingestr.src.snapchat_ads.helpers import parse_stats_table

                parsed = parse_stats_table(table)
                resource_name = parsed.resource_name
                # Build stats_config dict from ParsedStatsTable
                stats_config = {
                    "granularity": parsed.granularity,
                    "fields": parsed.fields,
                }
                if parsed.breakdown:
                    stats_config["breakdown"] = parsed.breakdown
                if parsed.dimension:
                    stats_config["dimension"] = parsed.dimension
                if parsed.pivot:
                    stats_config["pivot"] = parsed.pivot
            else:
                # Non-stats table with ad_account_id(s): resource_name:id1,id2,id3
                ad_account_ids_str = parts[1] if len(parts) > 1 else None
                if not ad_account_ids_str:
                    raise ValueError(
                        f"ad_account_id must be provided in format '{resource_name}:ad_account_id' or '{resource_name}:id1,id2,id3'"
                    )
                ad_account_id = [
                    id.strip() for id in ad_account_ids_str.split(",") if id.strip()
                ]
        else:
            resource_name = table
            if resource_name in self.STATS_RESOURCES:
                # Stats resource with default config
                stats_config = {
                    "granularity": "DAY",
                    "fields": "impressions,spend",
                }

        # Validation for non-stats resources
        if resource_name not in self.STATS_RESOURCES:
            account_id_required = (
                resource_name in self.AD_ACCOUNT_RESOURCES
                and ad_account_id is None
                and not organization_id
            )
            if account_id_required:
                raise ValueError(
                    f"organization_id is required for '{resource_name}' table when no specific ad_account_id is provided"
                )

            if not organization_id and table != "organizations":
                raise ValueError(
                    f"organization_id is required for table '{table}'. Only 'organizations' table does not require organization_id."
                )
        else:
            # Stats resources require organization_id
            if not organization_id:
                raise ValueError(f"organization_id is required for '{resource_name}'")

        if resource_name not in self.resources:
            raise UnsupportedResourceError(table, "Snapchat Ads")

        from ingestr.src.snapchat_ads import snapchat_ads_source

        source_kwargs: dict[str, Any] = {
            "refresh_token": refresh_token[0],
            "client_id": client_id[0],
            "client_secret": client_secret[0],
        }

        if organization_id:
            source_kwargs["organization_id"] = organization_id[0]

        # Only pass ad_account_id for non-stats resources
        if ad_account_id and resource_name not in self.STATS_RESOURCES:
            source_kwargs["ad_account_id"] = ad_account_id

        # Add interval_start and interval_end for client-side filtering
        interval_start = kwargs.get("interval_start")
        if interval_start:
            source_kwargs["start_date"] = interval_start

        interval_end = kwargs.get("interval_end")
        if interval_end:
            source_kwargs["end_date"] = interval_end

        # Add stats_config for stats resource
        if stats_config:
            source_kwargs["stats_config"] = stats_config

        source = snapchat_ads_source(**source_kwargs)

        return source.with_resources(resource_name)


class BruinSource:
    # bruin://?api_token=<api_token>
    def handles_incrementality(self) -> bool:
        return False

    def dlt_source(self, uri: str, table: str, **kwargs):
        parsed_uri = urlparse(uri)
        params = parse_qs(parsed_uri.query)

        api_token = params.get("api_token")
        if api_token is None:
            raise MissingValueError("api_token", "Bruin")

        if table not in ["pipelines", "assets"]:
            raise UnsupportedResourceError(table, "Bruin")

        from ingestr.src.bruin import bruin_source

        return bruin_source(api_token=api_token[0]).with_resources(table)


class PrimerSource:
    # primer://?api_key=<api_key>
    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        parsed_uri = urlparse(uri)
        params = parse_qs(parsed_uri.query)

        api_key = params.get("api_key")
        if api_key is None:
            raise MissingValueError("api_key", "Primer")
        if table not in ["payments"]:
            raise UnsupportedResourceError(table, "Primer")

        date_args: dict[str, str] = {}
        if kwargs.get("interval_start"):
            date_args["start_date"] = kwargs["interval_start"]
        if kwargs.get("interval_end"):
            date_args["end_date"] = kwargs["interval_end"]

        from ingestr.src.primer import primer_source

        return primer_source(
            api_key=api_key[0],
            **date_args,
        ).with_resources(table)

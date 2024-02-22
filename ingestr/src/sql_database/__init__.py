"""Source that loads tables form any SQLAlchemy supported database, supports batching requests and incremental loads."""

from typing import Any, Optional, Union

import dlt
from dlt.sources import DltResource
from dlt.sources.credentials import ConnectionStringCredentials
from sqlalchemy import MetaData, Table
from sqlalchemy.engine import Engine

from .helpers import (
    engine_from_credentials,
    get_primary_key,
    table_rows,
)
from .schema_types import table_to_columns


def sql_table(
    credentials: Union[ConnectionStringCredentials, Engine, str] = dlt.secrets.value,
    table: str = dlt.config.value,
    schema: Optional[str] = dlt.config.value,
    metadata: Optional[MetaData] = None,
    incremental: Optional[dlt.sources.incremental[Any]] = None,
    detect_precision_hints: Optional[bool] = dlt.config.value,
    merge_key: Optional[str] = None,
) -> DltResource:
    """
    A dlt resource which loads data from an SQL database table using SQLAlchemy.

    Args:
        credentials (Union[ConnectionStringCredentials, Engine, str]): Database credentials or an `Engine` instance representing the database connection.
        table (str): Name of the table to load.
        schema (Optional[str]): Optional name of the schema the table belongs to.
        metadata (Optional[MetaData]): Optional `sqlalchemy.MetaData` instance. If provided, the `schema` argument is ignored.
        incremental (Optional[dlt.sources.incremental[Any]]): Option to enable incremental loading for the table.
            E.g., `incremental=dlt.sources.incremental('updated_at', pendulum.parse('2022-01-01T00:00:00Z'))`
        write_disposition (str): Write disposition of the resource.
        detect_precision_hints (bool): Set column precision and scale hints for supported data types in the target schema based on the columns in the source tables.
            This is disabled by default.

    Returns:
        DltResource: The dlt resource for loading data from the SQL database table.
    """
    if not isinstance(credentials, Engine):
        engine = engine_from_credentials(credentials)
    else:
        engine = credentials
    engine.execution_options(stream_results=True)
    metadata = metadata or MetaData(schema=schema)

    table_obj = Table(table, metadata, autoload_with=engine)

    return dlt.resource(
        table_rows,
        name=table_obj.name,
        primary_key=get_primary_key(table_obj),
        columns=table_to_columns(table_obj) if detect_precision_hints else None,  # type: ignore
        merge_key=merge_key,  # type: ignore
    )(engine, table_obj, incremental=incremental)

"""Source that loads tables form any SQLAlchemy supported database, supports batching requests and incremental loads."""

from typing import Any, Callable, Dict, Iterable, List, Optional, Union

import dlt
from dlt.common.configuration.specs.config_section_context import ConfigSectionContext
from dlt.sources import DltResource
from sqlalchemy import MetaData, Table
from sqlalchemy.engine import Engine

from .helpers import (
    SqlDatabaseTableConfiguration,
    SqlTableResourceConfiguration,
    TableBackend,
    engine_from_credentials,
    get_primary_key,
    table_rows,
)
from .override import IngestrConnectionStringCredentials
from .schema_types import table_to_columns


@dlt.source
def sql_database(
    credentials: Union[
        IngestrConnectionStringCredentials, Engine, str
    ] = dlt.secrets.value,
    schema: Optional[str] = dlt.config.value,
    metadata: Optional[MetaData] = None,
    table_names: Optional[List[str]] = dlt.config.value,
    chunk_size: int = 50000,
    backend: TableBackend = "sqlalchemy",
    detect_precision_hints: Optional[bool] = dlt.config.value,
    defer_table_reflect: Optional[bool] = dlt.config.value,
    table_adapter_callback: Callable[[Table], None] = None,
    backend_kwargs: Dict[str, Any] = None,
) -> Iterable[DltResource]:
    """
    A dlt source which loads data from an SQL database using SQLAlchemy.
    Resources are automatically created for each table in the schema or from the given list of tables.

    Args:
        credentials (Union[IngestrConnectionStringCredentials, Engine, str]): Database credentials or an `sqlalchemy.Engine` instance.
        schema (Optional[str]): Name of the database schema to load (if different from default).
        metadata (Optional[MetaData]): Optional `sqlalchemy.MetaData` instance. `schema` argument is ignored when this is used.
        table_names (Optional[List[str]]): A list of table names to load. By default, all tables in the schema are loaded.
        chunk_size (int): Number of rows yielded in one batch. SQL Alchemy will create additional internal rows buffer twice the chunk size.
        backend (TableBackend): Type of backend to generate table data. One of: "sqlalchemy", "pyarrow", "pandas" and "connectorx".
            "sqlalchemy" yields batches as lists of Python dictionaries, "pyarrow" and "connectorx" yield batches as arrow tables, "pandas" yields panda frames.
            "sqlalchemy" is the default and does not require additional dependencies, "pyarrow" creates stable destination schemas with correct data types,
            "connectorx" is typically the fastest but ignores the "chunk_size" so you must deal with large tables yourself.
        detect_precision_hints (bool): Set column precision and scale hints for supported data types in the target schema based on the columns in the source tables.
            This is disabled by default.
        defer_table_reflect (bool): Will connect and reflect table schema only when yielding data. Requires table_names to be explicitly passed.
            Enable this option when running on Airflow. Available on dlt 0.4.4 and later.
        table_adapter_callback: (Callable): Receives each reflected table. May be used to modify the list of columns that will be selected.
        backend_kwargs (**kwargs): kwargs passed to table backend ie. "conn" is used to pass specialized connection string to connectorx.
    Returns:
        Iterable[DltResource]: A list of DLT resources for each table to be loaded.
    """

    # set up alchemy engine
    engine = engine_from_credentials(credentials)
    engine.execution_options(stream_results=True, max_row_buffer=2 * chunk_size)
    metadata = metadata or MetaData(schema=schema)

    # use provided tables or all tables
    if table_names:
        tables = [
            Table(name, metadata, autoload_with=None if defer_table_reflect else engine)
            for name in table_names
        ]
    else:
        if defer_table_reflect:
            raise ValueError("You must pass table names to defer table reflection")
        metadata.reflect(bind=engine)
        tables = list(metadata.tables.values())

    for table in tables:
        if table_adapter_callback and not defer_table_reflect:
            table_adapter_callback(table)
        yield dlt.resource(
            table_rows,
            name=table.name,
            primary_key=get_primary_key(table),
            spec=SqlDatabaseTableConfiguration,
            columns=table_to_columns(table, detect_precision_hints),
        )(
            engine,
            table,
            chunk_size,
            backend,
            detect_precision_hints=detect_precision_hints,
            defer_table_reflect=defer_table_reflect,
            table_adapter_callback=table_adapter_callback,
            backend_kwargs=backend_kwargs,
        )


@dlt.sources.config.with_config(
    sections=("sources", "sql_database"),
    spec=SqlTableResourceConfiguration,
    sections_merge_style=ConfigSectionContext.resource_merge_style,
)
def sql_table(
    credentials: Union[
        IngestrConnectionStringCredentials, Engine, str
    ] = dlt.secrets.value,
    table: str = dlt.config.value,
    schema: Optional[str] = dlt.config.value,
    metadata: Optional[MetaData] = None,
    incremental: Optional[dlt.sources.incremental[Any]] = None,
    chunk_size: int = 1000,
    backend: TableBackend = "sqlalchemy",
    detect_precision_hints: Optional[bool] = dlt.config.value,
    defer_table_reflect: Optional[bool] = dlt.config.value,
    table_adapter_callback: Callable[[Table], None] = None,
    backend_kwargs: Dict[str, Any] = None,
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
        chunk_size (int): Number of rows yielded in one batch. SQL Alchemy will create additional internal rows buffer twice the chunk size.
        backend (TableBackend): Type of backend to generate table data. One of: "sqlalchemy", "pyarrow", "pandas" and "connectorx".
            "sqlalchemy" yields batches as lists of Python dictionaries, "pyarrow" and "connectorx" yield batches as arrow tables, "pandas" yields panda frames.
            "sqlalchemy" is the default and does not require additional dependencies, "pyarrow" creates stable destination schemas with correct data types,
            "connectorx" is typically the fastest but ignores the "chunk_size" so you must deal with large tables yourself.
        detect_precision_hints (bool): Set column precision and scale hints for supported data types in the target schema based on the columns in the source tables.
            This is disabled by default.
        defer_table_reflect (bool): Will connect and reflect table schema only when yielding data. Enable this option when running on Airflow. Available
            on dlt 0.4.4 and later
        table_adapter_callback: (Callable): Receives each reflected table. May be used to modify the list of columns that will be selected.
        backend_kwargs (**kwargs): kwargs passed to table backend ie. "conn" is used to pass specialized connection string to connectorx.

    Returns:
        DltResource: The dlt resource for loading data from the SQL database table.
    """
    engine = engine_from_credentials(credentials)
    engine.execution_options(stream_results=True, max_row_buffer=2 * chunk_size)
    metadata = metadata or MetaData(schema=schema)

    table_obj = Table(
        table, metadata, autoload_with=None if defer_table_reflect else engine
    )
    if table_adapter_callback and not defer_table_reflect:
        table_adapter_callback(table_obj)

    return dlt.resource(
        table_rows,
        name=table_obj.name,
        primary_key=get_primary_key(table_obj),
        columns=table_to_columns(table_obj, detect_precision_hints),
        merge_key=merge_key,
    )(
        engine,
        table_obj,
        chunk_size,
        backend,
        incremental=incremental,
        detect_precision_hints=detect_precision_hints,
        defer_table_reflect=defer_table_reflect,
        table_adapter_callback=table_adapter_callback,
        backend_kwargs=backend_kwargs,
    )

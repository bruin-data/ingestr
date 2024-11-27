#
# Copyright (c) 2012-2023 Snowflake Computing Inc. All rights reserved.
#

from __future__ import annotations

import collections.abc
import os
import warnings
from functools import partial
from logging import getLogger
from tempfile import TemporaryDirectory
from typing import (
    TYPE_CHECKING,
    Any,
    Callable,
    Iterable,
    Iterator,
    Literal,
    Sequence,
    TypeVar,
)

from snowflake.connector import ProgrammingError
from snowflake.connector.options import pandas
from snowflake.connector.telemetry import TelemetryData, TelemetryField
from snowflake.connector.util_text import random_string

from ._utils import (
    _PYTHON_SNOWPARK_USE_SCOPED_TEMP_OBJECTS_STRING,
    TempObjectType,
    get_temp_type_for_object,
    random_name_for_temp_object,
)
from .cursor import SnowflakeCursor

if TYPE_CHECKING:  # pragma: no cover
    from .connection import SnowflakeConnection

    try:
        import sqlalchemy
    except ImportError:
        sqlalchemy = None

T = TypeVar("T", bound=collections.abc.Sequence)

logger = getLogger(__name__)


def chunk_helper(
    lst: pandas.DataFrame, n: int
) -> Iterator[tuple[int, pandas.DataFrame]]:
    """Helper generator to chunk a sequence efficiently with current index like if enumerate was called on sequence."""
    if len(lst) == 0:
        yield 0, lst
        return
    for i in range(0, len(lst), n):
        yield int(i / n), lst.iloc[i : i + n]


def build_location_helper(
    database: str | None, schema: str | None, name: str, quote_identifiers: bool
) -> str:
    """Helper to format table/stage/file format's location."""
    if quote_identifiers:
        location = (
            (('"' + database + '".') if database else "")
            + (('"' + schema + '".') if schema else "")
            + ('"' + name + '"')
        )
    else:
        location = (
            (database + "." if database else "")
            + (schema + "." if schema else "")
            + name
        )
    return location


def _do_create_temp_stage(
    cursor: SnowflakeCursor,
    stage_location: str,
    compression: str,
    auto_create_table: bool,
    overwrite: bool,
    use_scoped_temp_object: bool,
) -> None:
    create_stage_sql = f"CREATE {get_temp_type_for_object(use_scoped_temp_object)} STAGE /* Python:snowflake.connector.pandas_tools.write_pandas() */ {stage_location} FILE_FORMAT=(TYPE=PARQUET COMPRESSION={compression}{' BINARY_AS_TEXT=FALSE' if auto_create_table or overwrite else ''})"
    logger.debug(f"creating stage with '{create_stage_sql}'")
    cursor.execute(create_stage_sql, _is_internal=True).fetchall()


def _create_temp_stage(
    cursor: SnowflakeCursor,
    database: str | None,
    schema: str | None,
    quote_identifiers: bool,
    compression: str,
    auto_create_table: bool,
    overwrite: bool,
    use_scoped_temp_object: bool = False,
) -> str:
    stage_name = (
        random_name_for_temp_object(TempObjectType.STAGE)
        if use_scoped_temp_object
        else random_string()
    )
    stage_location = build_location_helper(
        database=database,
        schema=schema,
        name=stage_name,
        quote_identifiers=quote_identifiers,
    )
    try:
        _do_create_temp_stage(
            cursor,
            stage_location,
            compression,
            auto_create_table,
            overwrite,
            use_scoped_temp_object,
        )
    except ProgrammingError as e:
        # User may not have the privilege to create stage on the target schema, so fall back to use current schema as
        # the old behavior.
        logger.debug(
            f"creating stage {stage_location} failed. Exception {str(e)}. Fall back to use current schema"
        )
        stage_location = stage_name
        _do_create_temp_stage(
            cursor,
            stage_location,
            compression,
            auto_create_table,
            overwrite,
            use_scoped_temp_object,
        )

    return stage_location


def _do_create_temp_file_format(
    cursor: SnowflakeCursor,
    file_format_location: str,
    compression: str,
    sql_use_logical_type: str,
    use_scoped_temp_object: bool,
) -> None:
    file_format_sql = (
        f"CREATE {get_temp_type_for_object(use_scoped_temp_object)} FILE FORMAT {file_format_location} "
        f"/* Python:snowflake.connector.pandas_tools.write_pandas() */ "
        f"TYPE=PARQUET COMPRESSION={compression}{sql_use_logical_type}"
    )
    logger.debug(f"creating file format with '{file_format_sql}'")
    cursor.execute(file_format_sql, _is_internal=True)


def _create_temp_file_format(
    cursor: SnowflakeCursor,
    database: str | None,
    schema: str | None,
    quote_identifiers: bool,
    compression: str,
    sql_use_logical_type: str,
    use_scoped_temp_object: bool = False,
) -> str:
    file_format_name = (
        random_name_for_temp_object(TempObjectType.FILE_FORMAT)
        if use_scoped_temp_object
        else random_string()
    )
    file_format_location = build_location_helper(
        database=database,
        schema=schema,
        name=file_format_name,
        quote_identifiers=quote_identifiers,
    )
    try:
        _do_create_temp_file_format(
            cursor,
            file_format_location,
            compression,
            sql_use_logical_type,
            use_scoped_temp_object,
        )
    except ProgrammingError as e:
        # User may not have the privilege to create file format on the target schema, so fall back to use current schema
        # as the old behavior.
        logger.debug(
            f"creating stage {file_format_location} failed. Exception {str(e)}. Fall back to use current schema"
        )
        file_format_location = file_format_name
        _do_create_temp_file_format(
            cursor,
            file_format_location,
            compression,
            sql_use_logical_type,
            use_scoped_temp_object,
        )

    return file_format_location


def write_pandas(
    conn: SnowflakeConnection,
    df: pandas.DataFrame,
    table_name: str,
    database: str | None = None,
    schema: str | None = None,
    chunk_size: int | None = None,
    compression: str = "gzip",
    on_error: str = "abort_statement",
    parallel: int = 4,
    quote_identifiers: bool = True,
    auto_create_table: bool = False,
    create_temp_table: bool = False,
    overwrite: bool = False,
    table_type: Literal["", "temp", "temporary", "transient"] = "",
    use_logical_type: bool | None = None,
    **kwargs: Any,
) -> tuple[
    bool,
    int,
    int,
    Sequence[
        tuple[
            str,
            str,
            int,
            int,
            int,
            int,
            str | None,
            int | None,
            int | None,
            str | None,
        ]
    ],
]:
    """Allows users to most efficiently write back a pandas DataFrame to Snowflake.

    It works by dumping the DataFrame into Parquet files, uploading them and finally copying their data into the table.

    Returns whether all files were ingested correctly, number of chunks uploaded, and number of rows ingested
    with all of the COPY INTO command's output for debugging purposes.

        Example usage:
            import pandas
            from snowflake.connector.pandas_tools import write_pandas

            df = pandas.DataFrame([('Mark', 10), ('Luke', 20)], columns=['name', 'balance'])
            success, nchunks, nrows, _ = write_pandas(cnx, df, 'customers')

    Args:
        conn: Connection to be used to communicate with Snowflake.
        df: Dataframe we'd like to write back.
        table_name: Table name where we want to insert into.
        database: Database schema and table is in, if not provided the default one will be used (Default value = None).
        schema: Schema table is in, if not provided the default one will be used (Default value = None).
        chunk_size: Number of elements to be inserted once, if not provided all elements will be dumped once
            (Default value = None).
        compression: The compression used on the Parquet files, can only be gzip, or snappy. Gzip gives supposedly a
            better compression, while snappy is faster. Use whichever is more appropriate (Default value = 'gzip').
        on_error: Action to take when COPY INTO statements fail, default follows documentation at:
            https://docs.snowflake.com/en/sql-reference/sql/copy-into-table.html#copy-options-copyoptions
            (Default value = 'abort_statement').
        parallel: Number of threads to be used when uploading chunks, default follows documentation at:
            https://docs.snowflake.com/en/sql-reference/sql/put.html#optional-parameters (Default value = 4).
        quote_identifiers: By default, identifiers, specifically database, schema, table and column names
            (from df.columns) will be quoted. If set to False, identifiers are passed on to Snowflake without quoting.
            I.e. identifiers will be coerced to uppercase by Snowflake.  (Default value = True)
        auto_create_table: When true, will automatically create a table with corresponding columns for each column in
            the passed in DataFrame. The table will not be created if it already exists
        create_temp_table: (Deprecated) Will make the auto-created table as a temporary table
        overwrite: When true, and if auto_create_table is true, then it drops the table. Otherwise, it
        truncates the table. In both cases it will replace the existing contents of the table with that of the passed in
            Pandas DataFrame.
        table_type: The table type of to-be-created table. The supported table types include ``temp``/``temporary``
            and ``transient``. Empty means permanent table as per SQL convention.
        use_logical_type: Boolean that specifies whether to use Parquet logical types. With this file format option,
            Snowflake can interpret Parquet logical types during data loading. To enable Parquet logical types,
            set use_logical_type as True. Set to None to use Snowflakes default. For more information, see:
            https://docs.snowflake.com/en/sql-reference/sql/create-file-format


    Returns:
        Returns the COPY INTO command's results to verify ingestion in the form of a tuple of whether all chunks were
        ingested correctly, # of chunks, # of ingested rows, and ingest's output.
    """
    if database is not None and schema is None:
        raise ProgrammingError(
            "Schema has to be provided to write_pandas when a database is provided"
        )
    # This dictionary maps the compression algorithm to Snowflake put copy into command type
    # https://docs.snowflake.com/en/sql-reference/sql/copy-into-table.html#type-parquet
    compression_map = {"gzip": "auto", "snappy": "snappy"}
    if compression not in compression_map.keys():
        raise ProgrammingError(
            f"Invalid compression '{compression}', only acceptable values are: {compression_map.keys()}"
        )

    _use_scoped_temp_object = (
        conn._session_parameters.get(
            _PYTHON_SNOWPARK_USE_SCOPED_TEMP_OBJECTS_STRING, False
        )
        if conn._session_parameters
        else False
    )

    if create_temp_table:
        warnings.warn(
            "create_temp_table is deprecated, we still respect this parameter when it is True but "
            'please consider using `table_type="temp"` instead',
            DeprecationWarning,
            # warnings.warn -> write_pandas
            stacklevel=2,
        )
        table_type = "temp"

    if table_type and table_type.lower() not in ["temp", "temporary", "transient"]:
        raise ValueError(
            "Unsupported table type. Expected table types: temp/temporary, transient"
        )

    if chunk_size is None:
        chunk_size = len(df)

    if not (
        isinstance(df.index, pandas.RangeIndex)
        and 1 == df.index.step
        and 0 == df.index.start
    ):
        warnings.warn(
            f"Pandas Dataframe has non-standard index of type {str(type(df.index))} which will not be written."
            f" Consider changing the index to pd.RangeIndex(start=0,...,step=1) or "
            f"call reset_index() to keep index as column(s)",
            UserWarning,
            stacklevel=2,
        )

    # use_logical_type should be True when dataframe contains datetimes with timezone.
    # https://github.com/snowflakedb/snowflake-connector-python/issues/1687
    if not use_logical_type and any(
        [pandas.api.types.is_datetime64tz_dtype(df[c]) for c in df.columns]
    ):
        warnings.warn(
            "Dataframe contains a datetime with timezone column, but "
            f"'{use_logical_type=}'. This can result in dateimes "
            "being incorrectly written to Snowflake. Consider setting "
            "'use_logical_type = True'",
            UserWarning,
            stacklevel=2,
        )

    if use_logical_type is None:
        sql_use_logical_type = ""
    elif use_logical_type:
        sql_use_logical_type = " USE_LOGICAL_TYPE = TRUE"
    else:
        sql_use_logical_type = " USE_LOGICAL_TYPE = FALSE"

    cursor = conn.cursor()
    stage_location = _create_temp_stage(
        cursor,
        database,
        schema,
        quote_identifiers,
        compression,
        auto_create_table,
        overwrite,
        _use_scoped_temp_object,
    )

    with TemporaryDirectory() as tmp_folder:
        for i, chunk in chunk_helper(df, chunk_size):
            chunk_path = os.path.join(tmp_folder, f"file{i}.txt")
            # Dump chunk into parquet file
            chunk.to_parquet(chunk_path, compression=compression, **kwargs)
            # Upload parquet file
            upload_sql = (
                "PUT /* Python:snowflake.connector.pandas_tools.write_pandas() */ "
                "'file://{path}' @{stage_location} PARALLEL={parallel}"
            ).format(
                path=chunk_path.replace("\\", "\\\\").replace("'", "\\'"),
                stage_location=stage_location,
                parallel=parallel,
            )
            logger.debug(f"uploading files with '{upload_sql}'")
            cursor.execute(upload_sql, _is_internal=True)
            # Remove chunk file
            os.remove(chunk_path)

    # in Snowflake, all parquet data is stored in a single column, $1, so we must select columns explicitly
    # see (https://docs.snowflake.com/en/user-guide/script-data-load-transform-parquet.html)
    if quote_identifiers:
        quote = '"'
        # if the column name contains a double quote, we need to escape it by replacing with two double quotes
        # https://docs.snowflake.com/en/sql-reference/identifiers-syntax#double-quoted-identifiers
        snowflake_column_names = [str(c).replace('"', '""') for c in df.columns]
    else:
        quote = ""
        snowflake_column_names = list(df.columns)
    columns = quote + f"{quote},{quote}".join(snowflake_column_names) + quote

    def drop_object(name: str, object_type: str) -> None:
        drop_sql = f"DROP {object_type.upper()} IF EXISTS {name} /* Python:snowflake.connector.pandas_tools.write_pandas() */"
        logger.debug(f"dropping {object_type} with '{drop_sql}'")
        cursor.execute(drop_sql, _is_internal=True)

    if auto_create_table or overwrite:
        file_format_location = _create_temp_file_format(
            cursor,
            database,
            schema,
            quote_identifiers,
            compression_map[compression],
            sql_use_logical_type,
            _use_scoped_temp_object,
        )
        infer_schema_sql = f"SELECT COLUMN_NAME, TYPE FROM table(infer_schema(location=>'@{stage_location}', file_format=>'{file_format_location}'))"
        logger.debug(f"inferring schema with '{infer_schema_sql}'")
        column_type_mapping = dict(
            cursor.execute(infer_schema_sql, _is_internal=True).fetchall()
        )
        # Infer schema can return the columns out of order depending on the chunking we do when uploading
        # so we have to iterate through the dataframe columns to make sure we create the table with its
        # columns in order
        create_table_columns = ", ".join(
            [
                f"{quote}{snowflake_col}{quote} {column_type_mapping[col]}"
                for snowflake_col, col in zip(snowflake_column_names, df.columns)
            ]
        )

        target_table_location = build_location_helper(
            database,
            schema,
            random_string() if (overwrite and auto_create_table) else table_name,
            quote_identifiers,
        )

        create_table_sql = (
            f"CREATE {table_type.upper()} TABLE IF NOT EXISTS {target_table_location} "
            f"({create_table_columns})"
            f" /* Python:snowflake.connector.pandas_tools.write_pandas() */ "
        )
        logger.debug(f"auto creating table with '{create_table_sql}'")
        cursor.execute(create_table_sql, _is_internal=True)
        # need explicit casting when the underlying table schema is inferred
        parquet_columns = "$1:" + ",$1:".join(
            f"{quote}{snowflake_col}{quote}::{column_type_mapping[col]}"
            for snowflake_col, col in zip(snowflake_column_names, df.columns)
        )
    else:
        target_table_location = build_location_helper(
            database=database,
            schema=schema,
            name=table_name,
            quote_identifiers=quote_identifiers,
        )
        parquet_columns = "$1:" + ",$1:".join(
            f"{quote}{snowflake_col}{quote}" for snowflake_col in snowflake_column_names
        )

    try:
        if overwrite and (not auto_create_table):
            truncate_sql = f"TRUNCATE TABLE {target_table_location} /* Python:snowflake.connector.pandas_tools.write_pandas() */"
            logger.debug(f"truncating table with '{truncate_sql}'")
            cursor.execute(truncate_sql, _is_internal=True)

        copy_into_sql = (
            f"COPY INTO {target_table_location} /* Python:snowflake.connector.pandas_tools.write_pandas() */ "
            f"({columns}) "
            f"FROM (SELECT {parquet_columns} FROM @{stage_location}) "
            f"FILE_FORMAT=("
            f"TYPE=PARQUET "
            f"COMPRESSION={compression_map[compression]}"
            f"{' BINARY_AS_TEXT=FALSE' if auto_create_table or overwrite else ''}"
            f"{sql_use_logical_type}"
            f") "
            f"PURGE=TRUE ON_ERROR={on_error}"
        )
        logger.debug(f"copying into with '{copy_into_sql}'")
        copy_results = cursor.execute(copy_into_sql, _is_internal=True).fetchall()

        if overwrite and auto_create_table:
            original_table_location = build_location_helper(
                database=database,
                schema=schema,
                name=table_name,
                quote_identifiers=quote_identifiers,
            )
            drop_object(original_table_location, "table")
            rename_table_sql = f"ALTER TABLE {target_table_location} RENAME TO {original_table_location} /* Python:snowflake.connector.pandas_tools.write_pandas() */"
            logger.debug(f"rename table with '{rename_table_sql}'")
            cursor.execute(rename_table_sql, _is_internal=True)
    except ProgrammingError:
        if overwrite and auto_create_table:
            # drop table only if we created a new one with a random name
            drop_object(target_table_location, "table")
        raise
    finally:
        cursor._log_telemetry_job_data(TelemetryField.PANDAS_WRITE, TelemetryData.TRUE)
        cursor.close()

    return (
        all(e[1] == "LOADED" for e in copy_results),
        len(copy_results),
        sum(int(e[3]) for e in copy_results),
        copy_results,
    )


def make_pd_writer(
    **kwargs,
) -> Callable[
    [
        pandas.io.sql.SQLTable,
        sqlalchemy.engine.Engine | sqlalchemy.engine.Connection,
        Iterable,
        Iterable,
        Any,
    ],
    None,
]:
    """This returns a pd_writer with the desired arguments.

        Example usage:
            import pandas as pd
            from snowflake.connector.pandas_tools import pd_writer

            sf_connector_version_df = pd.DataFrame([('snowflake-connector-python', '1.0')], columns=['NAME', 'NEWEST_VERSION'])
            sf_connector_version_df.to_sql('driver_versions', engine, index=False, method=make_pd_writer())

            # to use parallel=1, quote_identifiers=False,
            from functools import partial
            sf_connector_version_df.to_sql(
                'driver_versions', engine, index=False, method=make_pd_writer(parallel=1, quote_identifiers=False)))

    This function takes arguments used by 'pd_writer' (excluding 'table', 'conn', 'keys', and 'data_iter')
    Please refer to 'pd_writer' for documentation.
    """
    if any(arg in kwargs for arg in ("table", "conn", "keys", "data_iter")):
        raise ProgrammingError(
            "Arguments 'table', 'conn', 'keys', and 'data_iter' are not supported parameters for make_pd_writer."
        )

    return partial(pd_writer, **kwargs)


def pd_writer(
    table: pandas.io.sql.SQLTable,
    conn: sqlalchemy.engine.Engine | sqlalchemy.engine.Connection,
    keys: Iterable,
    data_iter: Iterable,
    **kwargs,
) -> None:
    """This is a wrapper on top of write_pandas to make it compatible with to_sql method in pandas.

        Notes:
            Please note that when column names in the pandas DataFrame are consist of strictly lower case letters, column names need to
            be enquoted, otherwise `ProgrammingError` will be raised.

            This is because `snowflake-sqlalchemy` does not enquote lower case column names when creating the table, but `pd_writer` enquotes the columns by default.
            the copy into command looks for enquoted column names.

            Future improvements will be made in the snowflake-sqlalchemy library.

        Example usage:
            import pandas as pd
            from snowflake.connector.pandas_tools import pd_writer

            sf_connector_version_df = pd.DataFrame([('snowflake-connector-python', '1.0')], columns=['NAME', 'NEWEST_VERSION'])
            sf_connector_version_df.to_sql('driver_versions', engine, index=False, method=pd_writer)

            # when the column names are consist of only lower case letters, enquote the column names
            sf_connector_version_df = pd.DataFrame([('snowflake-connector-python', '1.0')], columns=['"name"', '"newest_version"'])
            sf_connector_version_df.to_sql('driver_versions', engine, index=False, method=pd_writer)

    Args:
        table: Pandas package's table object.
        conn: SQLAlchemy engine object to talk to Snowflake.
        keys: Column names that we are trying to insert.
        data_iter: Iterator over the rows.

        More parameters can be provided to be used by 'write_pandas' (excluding 'conn', 'df', 'table_name', and 'schema'),
        Please refer to 'write_pandas' for documentation on other available parameters.
    """
    if any(arg in kwargs for arg in ("conn", "df", "table_name", "schema")):
        raise ProgrammingError(
            "Arguments 'conn', 'df', 'table_name', and 'schema' are not supported parameters for pd_writer."
        )

    sf_connection = conn.connection.connection
    df = pandas.DataFrame(data_iter, columns=keys)
    write_pandas(
        conn=sf_connection,
        df=df,
        # Note: Our sqlalchemy connector creates tables case insensitively
        table_name=table.name.upper(),
        schema=table.schema,
        **kwargs,
    )

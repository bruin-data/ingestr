# -*- coding: utf-8 -*-
from __future__ import annotations

import concurrent
import logging
import textwrap
import uuid
from collections import OrderedDict
from concurrent.futures.process import ProcessPoolExecutor
from concurrent.futures.thread import ThreadPoolExecutor
from copy import deepcopy
from multiprocessing import cpu_count
from typing import (
    TYPE_CHECKING,
    Any,
    Callable,
    Dict,
    Iterator,
    List,
    Optional,
    Type,
    Union,
)

from boto3 import Session

from pyathena import OperationalError
from pyathena.model import AthenaCompression
from pyathena.util import RetryConfig, parse_output_location, retry_api_call

if TYPE_CHECKING:
    from pandas import DataFrame, Series

    from pyathena.connection import Connection
    from pyathena.cursor import Cursor

_logger = logging.getLogger(__name__)  # type: ignore


def get_chunks(df: "DataFrame", chunksize: Optional[int] = None) -> Iterator["DataFrame"]:
    rows = len(df)
    if rows == 0:
        return
    if chunksize is None:
        chunksize = rows
    elif chunksize <= 0:
        raise ValueError("Chunk size argument must be greater than zero")

    chunks = int(rows / chunksize) + 1
    for i in range(chunks):
        start_i = i * chunksize
        end_i = min((i + 1) * chunksize, rows)
        if start_i >= end_i:
            break
        yield df[start_i:end_i]


def reset_index(df: "DataFrame", index_label: Optional[str] = None) -> None:
    df.index.name = index_label if index_label else "index"
    try:
        df.reset_index(inplace=True)
    except ValueError as e:
        raise ValueError(f"Duplicate name in index/columns: {e}")


def as_pandas(cursor: "Cursor", coerce_float: bool = False) -> "DataFrame":
    from pandas import DataFrame

    description = cursor.description
    if not description:
        return DataFrame()
    names = [metadata[0] for metadata in description]
    return DataFrame.from_records(cursor.fetchall(), columns=names, coerce_float=coerce_float)


def to_sql_type_mappings(col: "Series") -> str:
    import pandas as pd

    col_type = pd.api.types.infer_dtype(col, skipna=True)
    if col_type == "datetime64" or col_type == "datetime":
        return "TIMESTAMP"
    elif col_type == "timedelta":
        return "INT"
    elif col_type == "timedelta64":
        return "BIGINT"
    elif col_type == "floating":
        if col.dtype == "float32":
            return "FLOAT"
        else:
            return "DOUBLE"
    elif col_type == "integer":
        if col.dtype == "int32":
            return "INT"
        else:
            return "BIGINT"
    elif col_type == "boolean":
        return "BOOLEAN"
    elif col_type == "date":
        return "DATE"
    elif col_type == "bytes":
        return "BINARY"
    elif col_type in ["complex", "time"]:
        raise ValueError(f"Data type `{col_type}` is not supported")
    return "STRING"


def to_parquet(
    df: "DataFrame",
    bucket_name: str,
    prefix: str,
    retry_config: RetryConfig,
    session_kwargs: Dict[str, Any],
    client_kwargs: Dict[str, Any],
    compression: Optional[str] = None,
    flavor: str = "spark",
) -> str:
    import pyarrow as pa
    from pyarrow import parquet as pq

    session = Session(**session_kwargs)
    client = session.resource("s3", **client_kwargs)
    bucket = client.Bucket(bucket_name)
    table = pa.Table.from_pandas(df)
    buf = pa.BufferOutputStream()
    pq.write_table(table, buf, compression=compression, flavor=flavor)
    response = retry_api_call(
        bucket.put_object,
        config=retry_config,
        Body=buf.getvalue().to_pybytes(),
        Key=prefix + str(uuid.uuid4()),
    )
    return f"s3://{response.bucket_name}/{response.key}"


def to_sql(
    df: "DataFrame",
    name: str,
    conn: "Connection[Any]",
    location: str,
    schema: str = "default",
    index: bool = False,
    index_label: Optional[str] = None,
    partitions: Optional[List[str]] = None,
    chunksize: Optional[int] = None,
    if_exists: str = "fail",
    compression: Optional[str] = None,
    flavor: str = "spark",
    type_mappings: Callable[["Series"], str] = to_sql_type_mappings,
    executor_class: Type[Union[ThreadPoolExecutor, ProcessPoolExecutor]] = ThreadPoolExecutor,
    max_workers: int = (cpu_count() or 1) * 5,
    repair_table=True,
) -> None:
    # TODO Supports orc, avro, json, csv or tsv format
    if if_exists not in ("fail", "replace", "append"):
        raise ValueError(f"`{if_exists}` is not valid for if_exists")
    if compression is not None and not AthenaCompression.is_valid(compression):
        raise ValueError(f"`{compression}` is not valid for compression")
    if partitions is None:
        partitions = []
    if not location.endswith("/"):
        location += "/"
    for partition_key in partitions:
        if partition_key is None:
            raise ValueError(
                f"Partition key: `{partition_key}` is None, no data will be written to the table."
            )
        if df[partition_key].isnull().any():
            raise ValueError(
                f"Partition key: `{partition_key}` contains None values, "
                "no data will be written to the table."
            )

    bucket_name, key_prefix = parse_output_location(location)
    bucket = conn.session.resource(
        "s3", region_name=conn.region_name, **conn._client_kwargs
    ).Bucket(bucket_name)
    cursor = conn.cursor()

    table = cursor.execute(
        textwrap.dedent(
            f"""
            SELECT table_name
            FROM information_schema.tables
            WHERE table_schema = '{schema}'
            AND table_name = '{name}'
            """
        )
    ).fetchall()
    if if_exists == "fail":
        if table:
            raise OperationalError(f"Table `{schema}.{name}` already exists.")
    elif if_exists == "replace":
        if table:
            cursor.execute(
                textwrap.dedent(
                    f"""
                    DROP TABLE {schema}.{name}
                    """
                )
            )
            objects = bucket.objects.filter(Prefix=key_prefix)
            if list(objects.limit(1)):
                objects.delete()

    if index:
        reset_index(df, index_label)
    with executor_class(max_workers=max_workers) as e:
        futures = []
        session_kwargs = deepcopy(conn._session_kwargs)
        session_kwargs.update({"profile_name": conn.profile_name})
        client_kwargs = deepcopy(conn._client_kwargs)
        client_kwargs.update({"region_name": conn.region_name})
        partition_prefixes = []
        if partitions:
            for keys, group in df.groupby(by=partitions, observed=True):
                keys = keys if isinstance(keys, tuple) else (keys,)
                group = group.drop(partitions, axis=1)
                partition_prefix = "/".join([f"{key}={val}" for key, val in zip(partitions, keys)])
                partition_prefixes.append(
                    (
                        ", ".join([f"`{key}` = '{val}'" for key, val in zip(partitions, keys)]),
                        f"{location}{partition_prefix}/",
                    )
                )
                for chunk in get_chunks(group, chunksize):
                    futures.append(
                        e.submit(
                            to_parquet,
                            chunk,
                            bucket_name,
                            f"{key_prefix}{partition_prefix}/",
                            conn._retry_config,
                            session_kwargs,
                            client_kwargs,
                            compression,
                            flavor,
                        )
                    )
        else:
            for chunk in get_chunks(df, chunksize):
                futures.append(
                    e.submit(
                        to_parquet,
                        chunk,
                        bucket_name,
                        key_prefix,
                        conn._retry_config,
                        session_kwargs,
                        client_kwargs,
                        compression,
                        flavor,
                    )
                )
        for future in concurrent.futures.as_completed(futures):
            result = future.result()
            _logger.info(f"to_parquet: {result}")

    ddl = generate_ddl(
        df=df,
        name=name,
        location=location,
        schema=schema,
        partitions=partitions,
        compression=compression,
        type_mappings=type_mappings,
    )
    _logger.info(ddl)
    cursor.execute(ddl)
    if partitions and repair_table:
        for partition in partition_prefixes:
            add_partition = textwrap.dedent(
                f"""
                ALTER TABLE `{schema}`.`{name}`
                ADD IF NOT EXISTS PARTITION ({partition[0]}) LOCATION '{partition[1]}'
                """
            )
            _logger.info(add_partition)
            cursor.execute(add_partition)


def get_column_names_and_types(df: "DataFrame", type_mappings) -> "OrderedDict[str, str]":
    return OrderedDict(
        ((str(df.columns[i]), type_mappings(df.iloc[:, i])) for i in range(len(df.columns)))
    )


def generate_ddl(
    df: "DataFrame",
    name: str,
    location: str,
    schema: str = "default",
    partitions: Optional[List[str]] = None,
    compression: Optional[str] = None,
    type_mappings: Callable[["Series"], str] = to_sql_type_mappings,
) -> str:
    if partitions is None:
        partitions = []
    column_names_and_types = get_column_names_and_types(df, type_mappings)
    ddl = f"CREATE EXTERNAL TABLE IF NOT EXISTS `{schema}`.`{name}` (\n"
    ddl += ",\n".join(
        [
            f"`{col}` {type_}"
            for col, type_ in column_names_and_types.items()
            if col not in partitions
        ]
    )
    ddl += "\n)\n"
    if partitions:
        ddl += "PARTITIONED BY (\n"
        ddl += ",\n".join([f"`{p}` {column_names_and_types[p]}" for p in partitions])
        ddl += "\n)\n"
    ddl += "STORED AS PARQUET\n"
    ddl += f"LOCATION '{location}'\n"
    if compression:
        ddl += f"TBLPROPERTIES ('parquet.compress'='{compression.upper()}')\n"
    return ddl

# -*- coding: utf-8 -*-
from __future__ import annotations

import logging
from typing import (
    TYPE_CHECKING,
    Any,
    Callable,
    Dict,
    List,
    Optional,
    Tuple,
    Type,
    Union,
)

from pyathena import OperationalError
from pyathena.arrow.util import to_column_info
from pyathena.converter import Converter
from pyathena.error import ProgrammingError
from pyathena.model import AthenaQueryExecution
from pyathena.result_set import AthenaResultSet
from pyathena.util import RetryConfig, parse_output_location

if TYPE_CHECKING:
    from pyarrow import Table

    from pyathena.connection import Connection

_logger = logging.getLogger(__name__)  # type: ignore


class AthenaArrowResultSet(AthenaResultSet):
    DEFAULT_BLOCK_SIZE = 1024 * 1024 * 128

    _timestamp_parsers: List[str] = [
        "%Y-%m-%d",
        "%Y-%m-%d %H:%M:%S",
        "%Y-%m-%d %H:%M:%S %Z",
        "%Y-%m-%d %H:%M:%S %z",
        "%Y-%m-%d %H:%M:%S.%f",
        "%Y-%m-%d %H:%M:%S.%f %Z",
        "%Y-%m-%d %H:%M:%S.%f %z",
        "%Y-%m-%dT%H:%M:%S",
        "%Y-%m-%dT%H:%M:%S %Z",
        "%Y-%m-%dT%H:%M:%S %z",
        "%Y-%m-%dT%H:%M:%S.%f",
        "%Y-%m-%dT%H:%M:%S.%f %Z",
        "%Y-%m-%dT%H:%M:%S.%f %z",
    ]

    def __init__(
        self,
        connection: "Connection[Any]",
        converter: Converter,
        query_execution: AthenaQueryExecution,
        arraysize: int,
        retry_config: RetryConfig,
        block_size: Optional[int] = None,
        unload: bool = False,
        unload_location: Optional[str] = None,
        **kwargs,
    ) -> None:
        super().__init__(
            connection=connection,
            converter=converter,
            query_execution=query_execution,
            arraysize=1,  # Fetch one row to retrieve metadata
            retry_config=retry_config,
        )
        self._rows.clear()  # Clear pre_fetch data
        self._arraysize = arraysize
        self._block_size = block_size if block_size else self.DEFAULT_BLOCK_SIZE
        self._unload = unload
        self._unload_location = unload_location
        self._kwargs = kwargs
        self._fs = self.__s3_file_system()
        if self.state == AthenaQueryExecution.STATE_SUCCEEDED and self.output_location:
            self._table = self._as_arrow()
        else:
            import pyarrow as pa

            self._table = pa.Table.from_pydict(dict())
        self._batches = iter(self._table.to_batches(arraysize))

    def __s3_file_system(self):
        from pyarrow import fs

        connection = self.connection
        if "role_arn" in connection._kwargs and connection._kwargs["role_arn"]:
            external_id = connection._kwargs.get("external_id")
            fs = fs.S3FileSystem(
                role_arn=connection._kwargs["role_arn"],
                session_name=connection._kwargs["role_session_name"],
                external_id="" if external_id is None else external_id,
                load_frequency=connection._kwargs["duration_seconds"],
                region=connection.region_name,
            )
        elif connection.profile_name:
            profile = connection.session._session.full_config["profiles"][connection.profile_name]
            fs = fs.S3FileSystem(
                access_key=profile.get("aws_access_key_id", None),
                secret_key=profile.get("aws_secret_access_key", None),
                session_token=profile.get("aws_session_token", None),
                region=connection.region_name,
            )
        else:
            fs = fs.S3FileSystem(
                access_key=connection._kwargs.get("aws_access_key_id"),
                secret_key=connection._kwargs.get("aws_secret_access_key"),
                session_token=connection._kwargs.get("aws_session_token"),
                region=connection.region_name,
            )
        return fs

    @property
    def is_unload(self):
        return self._unload and self.query and self.query.strip().upper().startswith("UNLOAD")

    @property
    def timestamp_parsers(self) -> List[str]:
        from pyarrow.csv import ISO8601

        return [ISO8601] + self._timestamp_parsers

    @property
    def column_types(self) -> Dict[str, Type[Any]]:
        import pyarrow as pa

        converter_types = self._converter.types
        description = self.description if self.description else []
        return {
            d[0]: converter_types.get(d[1], pa.string())
            for d in description
            if d[1] in converter_types
        }

    @property
    def converters(self) -> Dict[str, Callable[[Optional[str]], Optional[Any]]]:
        description = self.description if self.description else []
        return {d[0]: self._converter.get(d[1]) for d in description}

    def _fetch(self) -> None:
        try:
            rows = next(self._batches)
        except StopIteration:
            return
        else:
            dict_rows = rows.to_pydict()
            column_names = dict_rows.keys()
            processed_rows = [
                tuple(self.converters[k](v) for k, v in zip(column_names, row))
                for row in zip(*dict_rows.values())
            ]
            self._rows.extend(processed_rows)

    def fetchone(
        self,
    ) -> Optional[Union[Tuple[Optional[Any], ...], Dict[Any, Optional[Any]]]]:
        if not self._rows:
            self._fetch()
        if not self._rows:
            return None
        if self._rownumber is None:
            self._rownumber = 0
        self._rownumber += 1
        return self._rows.popleft()

    def fetchmany(
        self, size: Optional[int] = None
    ) -> List[Union[Tuple[Optional[Any], ...], Dict[Any, Optional[Any]]]]:
        if not size or size <= 0:
            size = self._arraysize
        rows = []
        for _ in range(size):
            row = self.fetchone()
            if row:
                rows.append(row)
            else:
                break
        return rows

    def fetchall(
        self,
    ) -> List[Union[Tuple[Optional[Any], ...], Dict[Any, Optional[Any]]]]:
        rows = []
        while True:
            row = self.fetchone()
            if row:
                rows.append(row)
            else:
                break
        return rows

    def _read_csv(self) -> "Table":
        import pyarrow as pa
        from pyarrow import csv

        if not self.output_location:
            raise ProgrammingError("OutputLocation is none or empty.")
        if not self.output_location.endswith((".csv", ".txt")):
            return pa.Table.from_pydict(dict())
        if self.substatement_type and self.substatement_type.upper() in (
            "UPDATE",
            "DELETE",
            "MERGE",
            "VACUUM_TABLE",
        ):
            return pa.Table.from_pydict(dict())
        length = self._get_content_length()
        if length and self.output_location.endswith(".txt"):
            description = self.description if self.description else []
            column_names = [d[0] for d in description]
            read_opts = csv.ReadOptions(
                skip_rows=0,
                column_names=column_names,
                block_size=self._block_size,
                use_threads=True,
            )
            parse_opts = csv.ParseOptions(
                delimiter="\t",
                quote_char=False,
                double_quote=False,
                escape_char=False,
            )
        elif length and self.output_location.endswith(".csv"):
            read_opts = csv.ReadOptions(skip_rows=0, block_size=self._block_size, use_threads=True)
            parse_opts = csv.ParseOptions(
                delimiter=",",
                quote_char='"',
                double_quote=True,
                escape_char=False,
            )
        else:
            return pa.Table.from_pydict(dict())

        bucket, key = parse_output_location(self.output_location)
        try:
            return csv.read_csv(
                self._fs.open_input_stream(f"{bucket}/{key}"),
                read_options=read_opts,
                parse_options=parse_opts,
                convert_options=csv.ConvertOptions(
                    quoted_strings_can_be_null=False,
                    timestamp_parsers=self.timestamp_parsers,
                    column_types=self.column_types,
                ),
            )
        except Exception as e:
            _logger.exception(f"Failed to read {bucket}/{key}.")
            raise OperationalError(*e.args) from e

    def _read_parquet(self) -> "Table":
        import pyarrow as pa
        from pyarrow import parquet

        manifests = self._read_data_manifest()
        if not manifests:
            return pa.Table.from_pydict(dict())
        if not self._unload_location:
            self._unload_location = "/".join(manifests[0].split("/")[:-1]) + "/"

        bucket, key = parse_output_location(self._unload_location)
        try:
            dataset = parquet.ParquetDataset(f"{bucket}/{key}", filesystem=self._fs)
            return dataset.read(use_threads=True)
        except Exception as e:
            _logger.exception(f"Failed to read {bucket}/{key}.")
            raise OperationalError(*e.args) from e

    def _as_arrow(self) -> "Table":
        if self.is_unload:
            table = self._read_parquet()
            self._metadata = to_column_info(table.schema)
        else:
            table = self._read_csv()
        return table

    def as_arrow(self) -> "Table":
        return self._table

    def close(self) -> None:
        import pyarrow as pa

        super().close()
        self._table = pa.Table.from_pydict(dict())
        self._batches = []

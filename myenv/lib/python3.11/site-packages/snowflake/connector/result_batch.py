#
# Copyright (c) 2012-2023 Snowflake Computing Inc. All rights reserved.
#

from __future__ import annotations

import abc
import json
import time
from base64 import b64decode
from enum import Enum, unique
from logging import getLogger
from typing import TYPE_CHECKING, Any, Callable, Iterator, NamedTuple, Sequence

from .arrow_context import ArrowConverterContext
from .backoff_policies import exponential_backoff
from .compat import OK, UNAUTHORIZED, urlparse
from .constants import FIELD_TYPES, IterUnit
from .errorcode import ER_FAILED_TO_CONVERT_ROW_TO_PYTHON_TYPE, ER_NO_PYARROW
from .errors import Error, InterfaceError, NotSupportedError, ProgrammingError
from .network import (
    RetryRequest,
    get_http_retryable_error,
    is_retryable_http_code,
    raise_failed_request_error,
    raise_okta_unauthorized_error,
)
from .options import installed_pandas
from .options import pyarrow as pa
from .secret_detector import SecretDetector
from .time_util import TimerContextManager
from .vendored import requests

logger = getLogger(__name__)

MAX_DOWNLOAD_RETRY = 10
DOWNLOAD_TIMEOUT = 7  # seconds

if TYPE_CHECKING:  # pragma: no cover
    from pandas import DataFrame
    from pyarrow import DataType, Table

    from .connection import SnowflakeConnection
    from .converter import SnowflakeConverterType
    from .cursor import ResultMetadataV2, SnowflakeCursor
    from .vendored.requests import Response


# emtpy pyarrow type array corresponding to FIELD_TYPES
FIELD_TYPE_TO_PA_TYPE: list[Callable[[ResultMetadataV2], DataType]] = []

# qrmk related constants
SSE_C_ALGORITHM = "x-amz-server-side-encryption-customer-algorithm"
SSE_C_KEY = "x-amz-server-side-encryption-customer-key"
SSE_C_AES = "AES256"


def _create_nanoarrow_iterator(
    data: bytes,
    context: ArrowConverterContext,
    use_dict_result: bool,
    numpy: bool,
    number_to_decimal: bool,
    row_unit: IterUnit,
):
    from .nanoarrow_arrow_iterator import PyArrowRowIterator, PyArrowTableIterator

    logger.debug("Using nanoarrow as the arrow data converter")
    return (
        PyArrowRowIterator(
            None,
            data,
            context,
            use_dict_result,
            numpy,
            number_to_decimal,
        )
        if row_unit == IterUnit.ROW_UNIT
        else PyArrowTableIterator(
            None,
            data,
            context,
            use_dict_result,
            numpy,
            number_to_decimal,
        )
    )


@unique
class DownloadMetrics(Enum):
    """Defines the keywords by which to store metrics for chunks."""

    download = "download"  # Download time in milliseconds
    parse = "parse"  # Parsing time to final data types
    load = "load"  # Parsing time from initial type to intermediate types


class RemoteChunkInfo(NamedTuple):
    """Small class that holds information about chunks that are given by back-end."""

    url: str
    uncompressedSize: int
    compressedSize: int


def create_batches_from_response(
    cursor: SnowflakeCursor,
    _format: str,
    data: dict[str, Any],
    schema: Sequence[ResultMetadataV2],
) -> list[ResultBatch]:
    column_converters: list[tuple[str, SnowflakeConverterType]] = []
    arrow_context: ArrowConverterContext | None = None
    rowtypes = data["rowtype"]
    total_len: int = data.get("total", 0)
    first_chunk_len = total_len
    rest_of_chunks: list[ResultBatch] = []
    if _format == "json":

        def col_to_converter(col: dict[str, Any]) -> tuple[str, SnowflakeConverterType]:
            type_name = col["type"].upper()
            python_method = cursor._connection.converter.to_python_method(
                type_name, col
            )
            return type_name, python_method

        column_converters = [col_to_converter(c) for c in rowtypes]
    else:
        rowset_b64 = data.get("rowsetBase64")
        arrow_context = ArrowConverterContext(cursor._connection._session_parameters)
    if "chunks" in data:
        chunks = data["chunks"]
        logger.debug(f"chunk size={len(chunks)}")
        # prepare the downloader for further fetch
        qrmk = data.get("qrmk")
        chunk_headers: dict[str, Any] = {}
        if "chunkHeaders" in data:
            chunk_headers = {}
            for header_key, header_value in data["chunkHeaders"].items():
                chunk_headers[header_key] = header_value
                if "encryption" not in header_key:
                    logger.debug(
                        f"added chunk header: key={header_key}, value={header_value}"
                    )
        elif qrmk is not None:
            logger.debug(f"qrmk={SecretDetector.mask_secrets(qrmk)}")
            chunk_headers[SSE_C_ALGORITHM] = SSE_C_AES
            chunk_headers[SSE_C_KEY] = qrmk

        def remote_chunk_info(c: dict[str, Any]) -> RemoteChunkInfo:
            return RemoteChunkInfo(
                url=c["url"],
                uncompressedSize=c["uncompressedSize"],
                compressedSize=c["compressedSize"],
            )

        if _format == "json":
            rest_of_chunks = [
                JSONResultBatch(
                    c["rowCount"],
                    chunk_headers,
                    remote_chunk_info(c),
                    schema,
                    column_converters,
                    cursor._use_dict_result,
                    json_result_force_utf8_decoding=cursor._connection._json_result_force_utf8_decoding,
                )
                for c in chunks
            ]
        else:
            rest_of_chunks = [
                ArrowResultBatch(
                    c["rowCount"],
                    chunk_headers,
                    remote_chunk_info(c),
                    arrow_context,
                    cursor._use_dict_result,
                    cursor._connection._numpy,
                    schema,
                    cursor._connection._arrow_number_to_decimal,
                )
                for c in chunks
            ]
    for c in rest_of_chunks:
        first_chunk_len -= c.rowcount
    if _format == "json":
        first_chunk = JSONResultBatch.from_data(
            data.get("rowset"),
            first_chunk_len,
            schema,
            column_converters,
            cursor._use_dict_result,
        )
    elif rowset_b64 is not None:
        first_chunk = ArrowResultBatch.from_data(
            rowset_b64,
            first_chunk_len,
            arrow_context,
            cursor._use_dict_result,
            cursor._connection._numpy,
            schema,
            cursor._connection._arrow_number_to_decimal,
        )
    else:
        logger.error(f"Don't know how to construct ResultBatches from response: {data}")
        first_chunk = ArrowResultBatch.from_data(
            "",
            0,
            arrow_context,
            cursor._use_dict_result,
            cursor._connection._numpy,
            schema,
            cursor._connection._arrow_number_to_decimal,
        )

    return [first_chunk] + rest_of_chunks


class ResultBatch(abc.ABC):
    """Represents what the back-end calls a result chunk.

    These are parts of a result set of a query. They each know how to retrieve their
    own results and convert them into Python native formats.

    As you are iterating through a ResultBatch you should check whether the yielded
    value is an ``Exception`` in case there was some error parsing the current row
    we might yield one of these to allow iteration to continue instead of raising the
    ``Exception`` when it occurs.

    These objects are pickleable for easy distribution and replication.

    Please note that the URLs stored in these do expire. The lifetime is dictated by the
    Snowflake back-end, at the time of writing this this is 6 hours.

    They can be iterated over multiple times and in different ways. Please follow the
    code in ``cursor.py`` to make sure that you are using this class correctly.

    """

    def __init__(
        self,
        rowcount: int,
        chunk_headers: dict[str, str] | None,
        remote_chunk_info: RemoteChunkInfo | None,
        schema: Sequence[ResultMetadataV2],
        use_dict_result: bool,
    ) -> None:
        self.rowcount = rowcount
        self._chunk_headers = chunk_headers
        self._remote_chunk_info = remote_chunk_info
        self._schema = schema
        self.schema = (
            [s._to_result_metadata_v1() for s in schema] if schema is not None else None
        )
        self._use_dict_result = use_dict_result
        self._metrics: dict[str, int] = {}
        self._data: str | list[tuple[Any, ...]] | None = None
        if self._remote_chunk_info:
            parsed_url = urlparse(self._remote_chunk_info.url)
            path_parts = parsed_url.path.rsplit("/", 1)
            self.id = path_parts[-1]
        else:
            self.id = str(self.rowcount)

    @property
    def _local(self) -> bool:
        """Whether this chunk is local."""
        return self._data is not None

    @property
    def compressed_size(self) -> int | None:
        """Returns the size of chunk in bytes in compressed form.

        If it's a local chunk this function returns None.
        """
        if self._local:
            return None
        return self._remote_chunk_info.compressedSize

    @property
    def uncompressed_size(self) -> int | None:
        """Returns the size of chunk in bytes in uncompressed form.

        If it's a local chunk this function returns None.
        """
        if self._local:
            return None
        return self._remote_chunk_info.uncompressedSize

    @property
    def column_names(self) -> list[str]:
        return [col.name for col in self._schema]

    def __iter__(
        self,
    ) -> Iterator[dict | Exception] | Iterator[tuple | Exception]:
        """Returns an iterator through the data this chunk holds.

        In case of this chunk being a local one it iterates through the local already
        parsed data and if it's a remote chunk it will download, parse its data and
        return an iterator through it.
        """
        return self.create_iter()

    def _download(
        self, connection: SnowflakeConnection | None = None, **kwargs
    ) -> Response:
        """Downloads the data that the ``ResultBatch`` is pointing at."""
        sleep_timer = 1
        backoff = (
            connection._backoff_generator
            if connection is not None
            else exponential_backoff()()
        )
        for retry in range(MAX_DOWNLOAD_RETRY):
            try:
                with TimerContextManager() as download_metric:
                    logger.debug(f"started downloading result batch id: {self.id}")
                    chunk_url = self._remote_chunk_info.url
                    request_data = {
                        "url": chunk_url,
                        "headers": self._chunk_headers,
                        "timeout": DOWNLOAD_TIMEOUT,
                    }
                    # Try to reuse a connection if possible
                    if connection and connection._rest is not None:
                        with connection._rest._use_requests_session() as session:
                            logger.debug(
                                f"downloading result batch id: {self.id} with existing session {session}"
                            )
                            response = session.request("get", **request_data)
                    else:
                        logger.debug(
                            f"downloading result batch id: {self.id} with new session"
                        )
                        response = requests.get(**request_data)

                    if response.status_code == OK:
                        logger.debug(
                            f"successfully downloaded result batch id: {self.id}"
                        )
                        break

                    # Raise error here to correctly go in to exception clause
                    if is_retryable_http_code(response.status_code):
                        # retryable server exceptions
                        error: Error = get_http_retryable_error(response.status_code)
                        raise RetryRequest(error)
                    elif response.status_code == UNAUTHORIZED:
                        # make a unauthorized error
                        raise_okta_unauthorized_error(None, response)
                    else:
                        raise_failed_request_error(None, chunk_url, "get", response)

            except (RetryRequest, Exception) as e:
                if retry == MAX_DOWNLOAD_RETRY - 1:
                    # Re-throw if we failed on the last retry
                    e = e.args[0] if isinstance(e, RetryRequest) else e
                    raise e
                sleep_timer = next(backoff)
                logger.exception(
                    f"Failed to fetch the large result set batch "
                    f"{self.id} for the {retry + 1} th time, "
                    f"backing off for {sleep_timer}s for the reason: '{e}'"
                )
                time.sleep(sleep_timer)

        self._metrics[DownloadMetrics.download.value] = (
            download_metric.get_timing_millis()
        )
        return response

    @abc.abstractmethod
    def create_iter(
        self, **kwargs
    ) -> (
        Iterator[dict | Exception]
        | Iterator[tuple | Exception]
        | Iterator[Table]
        | Iterator[DataFrame]
    ):
        """Downloads the data from from blob storage that this ResultChunk points at.

        This function is the one that does the actual work for ``self.__iter__``.

        It is necessary because a ``ResultBatch`` can return multiple types of
        iterators. A good example of this is simply iterating through
        ``SnowflakeCursor`` and calling ``fetch_pandas_batches`` on it.
        """
        raise NotImplementedError()

    def _check_can_use_pandas(self) -> None:
        if not installed_pandas:
            msg = (
                "Optional dependency: 'pandas' is not installed, please see the following link for install "
                "instructions: https://docs.snowflake.com/en/user-guide/python-connector-pandas.html#installation"
            )
            errno = ER_NO_PYARROW

            raise Error.errorhandler_make_exception(
                ProgrammingError,
                {
                    "msg": msg,
                    "errno": errno,
                },
            )

    @abc.abstractmethod
    def to_pandas(self) -> DataFrame:
        raise NotImplementedError()

    @abc.abstractmethod
    def to_arrow(self) -> Table:
        raise NotImplementedError()


class JSONResultBatch(ResultBatch):
    def __init__(
        self,
        rowcount: int,
        chunk_headers: dict[str, str] | None,
        remote_chunk_info: RemoteChunkInfo | None,
        schema: Sequence[ResultMetadataV2],
        column_converters: Sequence[tuple[str, SnowflakeConverterType]],
        use_dict_result: bool,
        *,
        json_result_force_utf8_decoding: bool = False,
    ) -> None:
        super().__init__(
            rowcount,
            chunk_headers,
            remote_chunk_info,
            schema,
            use_dict_result,
        )
        self._json_result_force_utf8_decoding = json_result_force_utf8_decoding
        self.column_converters = column_converters

    @classmethod
    def from_data(
        cls,
        data: Sequence[Sequence[Any]],
        data_len: int,
        schema: Sequence[ResultMetadataV2],
        column_converters: Sequence[tuple[str, SnowflakeConverterType]],
        use_dict_result: bool,
    ):
        """Initializes a ``JSONResultBatch`` from static, local data."""
        new_chunk = cls(
            len(data),
            None,
            None,
            schema,
            column_converters,
            use_dict_result,
        )
        new_chunk._data = new_chunk._parse(data)
        return new_chunk

    def _load(self, response: Response) -> list:
        """This function loads a compressed JSON file into memory.

        Returns:
            Whatever ``json.loads`` return, but in a list.
            Unfortunately there's no type hint for this.
            For context: https://github.com/python/typing/issues/182
        """
        # if users specify how to decode the data, we decode the bytes using the specified encoding
        if self._json_result_force_utf8_decoding:
            try:
                read_data = str(response.content, "utf-8", errors="strict")
            except Exception as exc:
                err_msg = f"failed to decode json result content due to error {exc!r}"
                logger.error(err_msg)
                raise Error(msg=err_msg)
        else:
            # note: SNOW-787480 response.apparent_encoding is unreliable, chardet.detect can be wrong which is used by
            # response.text to decode content, check issue: https://github.com/chardet/chardet/issues/148
            read_data = response.text
        return json.loads("".join(["[", read_data, "]"]))

    def _parse(
        self, downloaded_data
    ) -> list[dict | Exception] | list[tuple | Exception]:
        """Parses downloaded data into its final form."""
        logger.debug(f"parsing for result batch id: {self.id}")
        result_list = []
        if self._use_dict_result:
            for row in downloaded_data:
                row_result = {}
                try:
                    for (_t, c), v, col in zip(
                        self.column_converters,
                        row,
                        self._schema,
                    ):
                        row_result[col.name] = v if c is None or v is None else c(v)
                    result_list.append(row_result)
                except Exception as error:
                    msg = f"Failed to convert: field {col.name}: {_t}::{v}, Error: {error}"
                    logger.exception(msg)
                    result_list.append(
                        Error.errorhandler_make_exception(
                            InterfaceError,
                            {
                                "msg": msg,
                                "errno": ER_FAILED_TO_CONVERT_ROW_TO_PYTHON_TYPE,
                            },
                        )
                    )
        else:
            for row in downloaded_data:
                row_result = [None] * len(self._schema)
                try:
                    idx = 0
                    for (_t, c), v, _col in zip(
                        self.column_converters,
                        row,
                        self._schema,
                    ):
                        row_result[idx] = v if c is None or v is None else c(v)
                        idx += 1
                    result_list.append(tuple(row_result))
                except Exception as error:
                    msg = f"Failed to convert: field {_col.name}: {_t}::{v}, Error: {error}"
                    logger.exception(msg)
                    result_list.append(
                        Error.errorhandler_make_exception(
                            InterfaceError,
                            {
                                "msg": msg,
                                "errno": ER_FAILED_TO_CONVERT_ROW_TO_PYTHON_TYPE,
                            },
                        )
                    )
        return result_list

    def __repr__(self) -> str:
        return f"JSONResultChunk({self.id})"

    def create_iter(
        self, connection: SnowflakeConnection | None = None, **kwargs
    ) -> Iterator[dict | Exception] | Iterator[tuple | Exception]:
        if self._local:
            return iter(self._data)
        response = self._download(connection=connection)
        # Load data to a intermediate form
        logger.debug(f"started loading result batch id: {self.id}")
        with TimerContextManager() as load_metric:
            downloaded_data = self._load(response)
        logger.debug(f"finished loading result batch id: {self.id}")
        self._metrics[DownloadMetrics.load.value] = load_metric.get_timing_millis()
        # Process downloaded data
        with TimerContextManager() as parse_metric:
            parsed_data = self._parse(downloaded_data)
        self._metrics[DownloadMetrics.parse.value] = parse_metric.get_timing_millis()
        return iter(parsed_data)

    def _arrow_fetching_error(self):
        return NotSupportedError(
            f"Trying to use arrow fetching on {type(self)} which "
            f"is not ArrowResultChunk"
        )

    def to_pandas(self):
        raise self._arrow_fetching_error()

    def to_arrow(self):
        raise self._arrow_fetching_error()


class ArrowResultBatch(ResultBatch):
    def __init__(
        self,
        rowcount: int,
        chunk_headers: dict[str, str] | None,
        remote_chunk_info: RemoteChunkInfo | None,
        context: ArrowConverterContext,
        use_dict_result: bool,
        numpy: bool,
        schema: Sequence[ResultMetadataV2],
        number_to_decimal: bool,
    ) -> None:
        super().__init__(
            rowcount,
            chunk_headers,
            remote_chunk_info,
            schema,
            use_dict_result,
        )
        self._context = context
        self._numpy = numpy
        self._number_to_decimal = number_to_decimal

    def __repr__(self) -> str:
        return f"ArrowResultChunk({self.id})"

    def _load(
        self, response: Response, row_unit: IterUnit
    ) -> Iterator[dict | Exception] | Iterator[tuple | Exception]:
        """Creates a ``PyArrowIterator`` from a response.

        This is used to iterate through results in different ways depending on which
        mode that ``PyArrowIterator`` is in.
        """
        return _create_nanoarrow_iterator(
            response.content,
            self._context,
            self._use_dict_result,
            self._numpy,
            self._number_to_decimal,
            row_unit,
        )

    def _from_data(
        self, data: str, iter_unit: IterUnit
    ) -> Iterator[dict | Exception] | Iterator[tuple | Exception]:
        """Creates a ``PyArrowIterator`` files from a str.

        This is used to iterate through results in different ways depending on which
        mode that ``PyArrowIterator`` is in.
        """
        if len(data) == 0:
            return iter([])

        return _create_nanoarrow_iterator(
            b64decode(data),
            self._context,
            self._use_dict_result,
            self._numpy,
            self._number_to_decimal,
            iter_unit,
        )

    @classmethod
    def from_data(
        cls,
        data: str,
        data_len: int,
        context: ArrowConverterContext,
        use_dict_result: bool,
        numpy: bool,
        schema: Sequence[ResultMetadataV2],
        number_to_decimal: bool,
    ):
        """Initializes an ``ArrowResultBatch`` from static, local data."""
        new_chunk = cls(
            data_len,
            None,
            None,
            context,
            use_dict_result,
            numpy,
            schema,
            number_to_decimal,
        )
        new_chunk._data = data

        return new_chunk

    def _create_iter(
        self, iter_unit: IterUnit, connection: SnowflakeConnection | None = None
    ) -> Iterator[dict | Exception] | Iterator[tuple | Exception] | Iterator[Table]:
        """Create an iterator for the ResultBatch. Used by get_arrow_iter."""
        if self._local:
            try:
                return self._from_data(self._data, iter_unit)
            except Exception:
                if connection and getattr(connection, "_debug_arrow_chunk", False):
                    logger.debug(f"arrow data can not be parsed: {self._data}")
                raise
        response = self._download(connection=connection)
        logger.debug(f"started loading result batch id: {self.id}")
        with TimerContextManager() as load_metric:
            try:
                loaded_data = self._load(response, iter_unit)
            except Exception:
                if connection and getattr(connection, "_debug_arrow_chunk", False):
                    logger.debug(f"arrow data can not be parsed: {response}")
                raise
        logger.debug(f"finished loading result batch id: {self.id}")
        self._metrics[DownloadMetrics.load.value] = load_metric.get_timing_millis()
        return loaded_data

    def _get_arrow_iter(
        self, connection: SnowflakeConnection | None = None
    ) -> Iterator[Table]:
        """Returns an iterator for this batch which yields a pyarrow Table"""
        return self._create_iter(iter_unit=IterUnit.TABLE_UNIT, connection=connection)

    def _create_empty_table(self) -> Table:
        """Returns empty Arrow table based on schema"""
        if installed_pandas:
            # initialize pyarrow type array corresponding to FIELD_TYPES
            FIELD_TYPE_TO_PA_TYPE = [e.pa_type for e in FIELD_TYPES]
        fields = [
            pa.field(s.name, FIELD_TYPE_TO_PA_TYPE[s.type_code](s))
            for s in self._schema
        ]
        return pa.schema(fields).empty_table()

    def to_arrow(self, connection: SnowflakeConnection | None = None) -> Table:
        """Returns this batch as a pyarrow Table"""
        val = next(self._get_arrow_iter(connection=connection), None)
        if val is not None:
            return val
        return self._create_empty_table()

    def to_pandas(
        self, connection: SnowflakeConnection | None = None, **kwargs
    ) -> DataFrame:
        """Returns this batch as a pandas DataFrame"""
        self._check_can_use_pandas()
        table = self.to_arrow(connection=connection)
        return table.to_pandas(**kwargs)

    def _get_pandas_iter(
        self, connection: SnowflakeConnection | None = None, **kwargs
    ) -> Iterator[DataFrame]:
        """An iterator for this batch which yields a pandas DataFrame"""
        iterator_data = []
        dataframe = self.to_pandas(connection=connection, **kwargs)
        if not dataframe.empty:
            iterator_data.append(dataframe)
        return iter(iterator_data)

    def create_iter(
        self, connection: SnowflakeConnection | None = None, **kwargs
    ) -> (
        Iterator[dict | Exception]
        | Iterator[tuple | Exception]
        | Iterator[Table]
        | Iterator[DataFrame]
    ):
        """The interface used by ResultSet to create an iterator for this ResultBatch."""
        iter_unit: IterUnit = kwargs.pop("iter_unit", IterUnit.ROW_UNIT)
        if iter_unit == IterUnit.TABLE_UNIT:
            structure = kwargs.pop("structure", "pandas")
            if structure == "pandas":
                return self._get_pandas_iter(connection=connection, **kwargs)
            else:
                return self._get_arrow_iter(connection=connection)
        else:
            return self._create_iter(iter_unit=iter_unit, connection=connection)

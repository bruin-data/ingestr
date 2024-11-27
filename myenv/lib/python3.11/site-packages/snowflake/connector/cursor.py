#!/usr/bin/env python
#
# Copyright (c) 2012-2023 Snowflake Computing Inc. All rights reserved.
#

from __future__ import annotations

import collections
import logging
import os
import re
import signal
import sys
import time
import uuid
import warnings
from enum import Enum
from logging import getLogger
from threading import Lock
from types import TracebackType
from typing import (
    IO,
    TYPE_CHECKING,
    Any,
    Callable,
    Iterator,
    Literal,
    NamedTuple,
    NoReturn,
    Sequence,
    TypeVar,
    overload,
)

from typing_extensions import Self

from snowflake.connector.result_batch import create_batches_from_response
from snowflake.connector.result_set import ResultSet

from . import compat
from ._sql_util import get_file_transfer_type
from ._utils import _TrackedQueryCancellationTimer
from .bind_upload_agent import BindUploadAgent, BindUploadError
from .constants import (
    FIELD_NAME_TO_ID,
    PARAMETER_PYTHON_CONNECTOR_QUERY_RESULT_FORMAT,
    FileTransferType,
    QueryStatus,
)
from .errorcode import (
    ER_CURSOR_IS_CLOSED,
    ER_FAILED_PROCESSING_PYFORMAT,
    ER_FAILED_TO_REWRITE_MULTI_ROW_INSERT,
    ER_INVALID_VALUE,
    ER_NO_ARROW_RESULT,
    ER_NO_PYARROW,
    ER_NO_PYARROW_SNOWSQL,
    ER_NOT_POSITIVE_SIZE,
    ER_UNSUPPORTED_METHOD,
)
from .errors import (
    DatabaseError,
    Error,
    IntegrityError,
    InterfaceError,
    NotSupportedError,
    ProgrammingError,
)
from .options import installed_pandas
from .sqlstate import SQLSTATE_FEATURE_NOT_SUPPORTED
from .telemetry import TelemetryData, TelemetryField
from .time_util import get_time_millis

if TYPE_CHECKING:  # pragma: no cover
    from pandas import DataFrame
    from pyarrow import Table

    from .connection import SnowflakeConnection
    from .file_transfer_agent import SnowflakeProgressPercentage
    from .result_batch import ResultBatch

T = TypeVar("T", bound=collections.abc.Sequence)

logger = getLogger(__name__)


if not installed_pandas:
    logger.debug(
        "Failed to import pyarrow or pandas. Cannot use pandas fetch API. Please "
        "install snowflake-connector-python with the `pandas` extra to use these "
        "features."
    )


try:
    from .nanoarrow_arrow_iterator import PyArrowIterator  # NOQA

    CAN_USE_ARROW_RESULT_FORMAT = True
except ImportError as e:  # pragma: no cover
    logger.warning(
        f"Failed to import ArrowResult. No Apache Arrow result set format can be used. ImportError: {e}",
    )
    CAN_USE_ARROW_RESULT_FORMAT = False

STATEMENT_TYPE_ID_DML = 0x3000
STATEMENT_TYPE_ID_INSERT = STATEMENT_TYPE_ID_DML + 0x100
STATEMENT_TYPE_ID_UPDATE = STATEMENT_TYPE_ID_DML + 0x200
STATEMENT_TYPE_ID_DELETE = STATEMENT_TYPE_ID_DML + 0x300
STATEMENT_TYPE_ID_MERGE = STATEMENT_TYPE_ID_DML + 0x400
STATEMENT_TYPE_ID_MULTI_TABLE_INSERT = STATEMENT_TYPE_ID_DML + 0x500

STATEMENT_TYPE_ID_DML_SET = frozenset(
    [
        STATEMENT_TYPE_ID_DML,
        STATEMENT_TYPE_ID_INSERT,
        STATEMENT_TYPE_ID_UPDATE,
        STATEMENT_TYPE_ID_DELETE,
        STATEMENT_TYPE_ID_MERGE,
        STATEMENT_TYPE_ID_MULTI_TABLE_INSERT,
    ]
)

DESC_TABLE_RE = re.compile(r"desc(?:ribe)?\s+([\w_]+)\s*;?\s*$", flags=re.IGNORECASE)

LOG_MAX_QUERY_LENGTH = 80

ASYNC_NO_DATA_MAX_RETRY = 24
ASYNC_RETRY_PATTERN = [1, 1, 2, 3, 4, 8, 10]


class _NanoarrowUsage(str, Enum):
    # follow the session parameter to use nanoarrow converter or not
    FOLLOW_SESSION_PARAMETER = "follow_session_parameter"
    # ignore the session parameter, use nanoarrow converter
    ENABLE_NANOARROW = "enable_nanoarrow"
    # ignore the session parameter, do not use nanoarrow converter
    DISABLE_NANOARROW = "disable_nanoarrow"


class ResultMetadata(NamedTuple):
    name: str
    type_code: int
    display_size: int | None
    internal_size: int | None
    precision: int | None
    scale: int | None
    is_nullable: bool

    @classmethod
    def from_column(cls, col: dict[str, Any]):
        """Initializes a ResultMetadata object from the column description in the query response."""
        type_code = FIELD_NAME_TO_ID[
            (
                col["extTypeName"].upper()
                if col.get("extTypeName")
                else col["type"].upper()
            )
        ]

        return cls(
            col["name"],
            type_code,
            None,
            col["length"],
            col["precision"],
            col["scale"],
            col["nullable"],
        )


class ResultMetadataV2:
    """ResultMetadataV2 represents the type information of a single column.

    It is a replacement for ResultMetadata that contains additional attributes, currently
    `vector_dimension` and `fields`. This class will be unified with ResultMetadata in the
    near future.
    """

    def __init__(
        self,
        name: str,
        type_code: int,
        is_nullable: bool,
        display_size: int | None = None,
        internal_size: int | None = None,
        precision: int | None = None,
        scale: int | None = None,
        vector_dimension: int | None = None,
        fields: list[ResultMetadataV2] | None = None,
    ):
        self._name = name
        self._type_code = type_code
        self._is_nullable = is_nullable
        self._display_size = display_size
        self._internal_size = internal_size
        self._precision = precision
        self._scale = scale
        self._vector_dimension = vector_dimension
        self._fields = fields

    @classmethod
    def from_column(cls, col: dict[str, Any]) -> ResultMetadataV2:
        """Initializes a ResultMetadataV2 object from the column description in the query response.
        This differs from ResultMetadata in that it has newly-added fields which cannot be added to
        ResultMetadata since it is a named tuple.
        """
        col_type = (
            col["extTypeName"].upper()
            if col.get("extTypeName")
            else col["type"].upper()
        )

        fields = col.get("fields")
        processed_fields: Optional[List[ResultMetadataV2]] = None
        if fields is not None:
            if col_type in {"VECTOR", "ARRAY", "OBJECT", "MAP"}:
                processed_fields = [
                    ResultMetadataV2.from_column({"name": None, **f})
                    for f in col["fields"]
                ]
            else:
                raise ValueError(
                    f"Field parsing is not supported for columns of type {col_type}."
                )

        return cls(
            col["name"],
            FIELD_NAME_TO_ID[col_type],
            col["nullable"],
            None,
            col["length"],
            col["precision"],
            col["scale"],
            col.get("vectorDimension"),
            processed_fields,
        )

    def _to_result_metadata_v1(self):
        """Initializes a ResultMetadata object from a ResultMetadataV2 object.

        This method is for internal use only.
        """

        return ResultMetadata(
            self._name,
            self._type_code,
            self._display_size,
            self._internal_size,
            self._precision,
            self._scale,
            self._is_nullable,
        )

    def __str__(self) -> str:
        return (
            f"ResultMetadataV2(name={self._name},type_code={self._type_code},"
            + f"is_nullable={self._is_nullable},display_size={self._display_size},"
            + "internal_size={self._internal_size},precision={self._precision},"
            + "scale={self._scale},vector_dimension={self._vector_dimension},"
            + "fields={self.fields})"
        )

    def __eq__(self, other) -> bool:
        if not isinstance(other, self.__class__):
            return False

        return (
            self._name == other._name
            and self._type_code == other._type_code
            and self._is_nullable == other._is_nullable
            and self._display_size == other._display_size
            and self._internal_size == other._internal_size
            and self._precision == other._precision
            and self._scale == other._scale
            and self._vector_dimension == other._vector_dimension
            and self._fields == other._fields
        )

    @property
    def name(self) -> str:
        return self._name

    @property
    def type_code(self) -> int:
        return self._type_code

    @property
    def is_nullable(self) -> bool:
        return self._is_nullable

    @property
    def internal_size(self) -> int | None:
        return self._internal_size

    @property
    def display_size(self) -> int | None:
        return self._display_size

    @property
    def precision(self) -> int | None:
        return self._precision

    @property
    def scale(self) -> int | None:
        return self._scale

    @property
    def vector_dimension(self) -> int | None:
        return self._vector_dimension

    @property
    def fields(self) -> list[ResultMetadataV2] | None:
        return self._fields


def exit_handler(*_) -> NoReturn:
    """Handler for signal. When called, it will raise SystemExit with exit code FORCE_EXIT."""
    print("\nForce exit")
    logger.info("Force exit")
    sys.exit(1)


class ResultState(Enum):
    DEFAULT = 1
    VALID = 2
    RESET = 3


class SnowflakeCursor:
    """Implementation of Cursor object that is returned from Connection.cursor() method.

    Attributes:
        description: A list of namedtuples about metadata for all columns.
        rowcount: The number of records updated or selected. If not clear, -1 is returned.
        rownumber: The current 0-based index of the cursor in the result set or None if the index cannot be
            determined.
        sfqid: Snowflake query id in UUID form. Include this in the problem report to the customer support.
        sqlstate: Snowflake SQL State code.
        timestamp_output_format: Snowflake timestamp_output_format for timestamps.
        timestamp_ltz_output_format: Snowflake output format for LTZ timestamps.
        timestamp_tz_output_format: Snowflake output format for TZ timestamps.
        timestamp_ntz_output_format: Snowflake output format for NTZ timestamps.
        date_output_format: Snowflake output format for dates.
        time_output_format: Snowflake output format for times.
        timezone: Snowflake timezone.
        binary_output_format: Snowflake output format for binary fields.
        arraysize: The default number of rows fetched by fetchmany.
        connection: The connection object by which the cursor was created.
        errorhandle: The class that handles error handling.
        is_file_transfer: Whether, or not the current command is a put, or get.
    """

    # TODO:
    #    Most of these attributes have no reason to be properties, we could just store them in public variables.
    #    Calling a function is expensive in Python and most of these getters are unnecessary.

    INSERT_SQL_RE = re.compile(r"^insert\s+into", flags=re.IGNORECASE)
    COMMENT_SQL_RE = re.compile(r"/\*.*\*/")
    INSERT_SQL_VALUES_RE = re.compile(
        r".*VALUES\s*(\(.*\)).*", re.IGNORECASE | re.MULTILINE | re.DOTALL
    )
    ALTER_SESSION_RE = re.compile(
        r"alter\s+session\s+set\s+(\w*?)\s*=\s*\'?([^\']+?)\'?\s*(?:;|$)",
        flags=re.IGNORECASE | re.MULTILINE | re.DOTALL,
    )

    @staticmethod
    def get_file_transfer_type(sql: str) -> FileTransferType | None:
        """Decide whether a SQL is a file transfer and return its type.

        None is returned if the SQL isn't a file transfer so that this function can be
        used in an if-statement.
        """
        return get_file_transfer_type(sql)

    def __init__(
        self,
        connection: SnowflakeConnection,
        use_dict_result: bool = False,
    ) -> None:
        """Inits a SnowflakeCursor with a connection.

        Args:
            connection: The connection that created this cursor.
            use_dict_result: Decides whether to use dict result or not.
        """
        self._connection: SnowflakeConnection = connection

        self._errorhandler: Callable[
            [SnowflakeConnection, SnowflakeCursor, type[Error], dict[str, str]],
            None,
        ] = Error.default_errorhandler
        self.messages: list[
            tuple[type[Error] | type[Exception], dict[str, str | bool]]
        ] = []
        self._timebomb: _TrackedQueryCancellationTimer | None = (
            None  # must be here for abort_exit method
        )
        self._description: list[ResultMetadataV2] | None = None
        self._sfqid: str | None = None
        self._sqlstate = None
        self._total_rowcount = -1
        self._sequence_counter = -1
        self._request_id: uuid.UUID | None = None
        self._is_file_transfer = False
        self._multi_statement_resultIds: collections.deque[str] = collections.deque()
        self.multi_statement_savedIds: list[str] = []

        self._timestamp_output_format = None
        self._timestamp_ltz_output_format = None
        self._timestamp_ntz_output_format = None
        self._timestamp_tz_output_format = None
        self._date_output_format = None
        self._time_output_format = None
        self._timezone = None
        self._binary_output_format = None
        self._result: Iterator[tuple] | Iterator[dict] | None = None
        self._result_set: ResultSet | None = None
        self._result_state: ResultState = ResultState.DEFAULT
        self._use_dict_result = use_dict_result
        self.query: str | None = None
        # TODO: self._query_result_format could be defined as an enum
        self._query_result_format: str | None = None

        self._arraysize = 1  # PEP-0249: defaults to 1

        self._lock_canceling = Lock()

        self._first_chunk_time = None

        self._log_max_query_length = connection.log_max_query_length
        self._inner_cursor: SnowflakeCursor | None = None
        self._prefetch_hook = None
        self._rownumber: int | None = None

        self.reset()

    def __del__(self) -> None:  # pragma: no cover
        try:
            self.close()
        except compat.BASE_EXCEPTION_CLASS as e:
            if logger.getEffectiveLevel() <= logging.INFO:
                logger.info(e)

    @property
    def description(self) -> list[ResultMetadata]:
        if self._description is None:
            return None

        return [meta._to_result_metadata_v1() for meta in self._description]

    @property
    def _description_internal(self) -> list[ResultMetadataV2]:
        """Return the new format of result metadata for a query.

        This method is for internal use only.
        """
        return self._description

    @property
    def rowcount(self) -> int | None:
        return self._total_rowcount if self._total_rowcount >= 0 else None

    @property
    def rownumber(self) -> int | None:
        return self._rownumber if self._rownumber >= 0 else None

    @property
    def sfqid(self) -> str | None:
        return self._sfqid

    @property
    def sqlstate(self):
        return self._sqlstate

    @property
    def timestamp_output_format(self) -> str | None:
        return self._timestamp_output_format

    @property
    def timestamp_ltz_output_format(self) -> str | None:
        return (
            self._timestamp_ltz_output_format
            if self._timestamp_ltz_output_format
            else self._timestamp_output_format
        )

    @property
    def timestamp_tz_output_format(self) -> str | None:
        return (
            self._timestamp_tz_output_format
            if self._timestamp_tz_output_format
            else self._timestamp_output_format
        )

    @property
    def timestamp_ntz_output_format(self) -> str | None:
        return (
            self._timestamp_ntz_output_format
            if self._timestamp_ntz_output_format
            else self._timestamp_output_format
        )

    @property
    def date_output_format(self) -> str | None:
        return self._date_output_format

    @property
    def time_output_format(self) -> str | None:
        return self._time_output_format

    @property
    def timezone(self) -> str | None:
        return self._timezone

    @property
    def binary_output_format(self) -> str | None:
        return self._binary_output_format

    @property
    def arraysize(self) -> int:
        return self._arraysize

    @arraysize.setter
    def arraysize(self, value) -> None:
        self._arraysize = int(value)

    @property
    def connection(self) -> SnowflakeConnection:
        return self._connection

    @property
    def errorhandler(self) -> Callable:
        return self._errorhandler

    @errorhandler.setter
    def errorhandler(self, value: Callable | None) -> None:
        logger.debug("setting errorhandler: %s", value)
        if value is None:
            raise ProgrammingError("Invalid errorhandler is specified")
        self._errorhandler = value

    @property
    def is_file_transfer(self) -> bool:
        """Whether the command is PUT or GET."""
        return hasattr(self, "_is_file_transfer") and self._is_file_transfer

    @property
    def lastrowid(self) -> None:
        """Snowflake does not support lastrowid in which case None should be returned as per PEP249."""
        return None

    @overload
    def callproc(self, procname: str) -> tuple: ...

    @overload
    def callproc(self, procname: str, args: T) -> T: ...

    def callproc(self, procname: str, args=tuple()):
        """Call a stored procedure.

        Args:
            procname: The stored procedure to be called.
            args: Parameters to be passed into the stored procedure.

        Returns:
            The input parameters.
        """
        marker_format = "%s" if self._connection.is_pyformat else "?"
        command = (
            f"CALL {procname}({', '.join([marker_format for _ in range(len(args))])})"
        )
        self.execute(command, args)
        return args

    def close(self) -> bool | None:
        """Closes the cursor object.

        Returns whether the cursor was closed during this call.
        """
        try:
            if self.is_closed():
                return False
            with self._lock_canceling:
                self.reset(closing=True)
                self._connection = None
                del self.messages[:]
                return True
        except Exception:
            return None

    def is_closed(self) -> bool:
        return self._connection is None or self._connection.is_closed()

    def _execute_helper(
        self,
        query: str,
        timeout: int = 0,
        statement_params: dict[str, str] | None = None,
        binding_params: tuple | dict[str, dict[str, str]] = None,
        binding_stage: str | None = None,
        is_internal: bool = False,
        describe_only: bool = False,
        _no_results: bool = False,
        _is_put_get=None,
        _no_retry: bool = False,
        dataframe_ast: str | None = None,
    ) -> dict[str, Any]:
        del self.messages[:]

        if statement_params is not None and not isinstance(statement_params, dict):
            Error.errorhandler_wrapper(
                self.connection,
                self,
                ProgrammingError,
                {
                    "msg": "The data type of statement params is invalid. It must be dict.",
                    "errno": ER_INVALID_VALUE,
                },
            )

        # check if current installation include arrow extension or not,
        # if not, we set statement level query result format to be JSON
        if not CAN_USE_ARROW_RESULT_FORMAT:
            logger.debug("Cannot use arrow result format, fallback to json format")
            if statement_params is None:
                statement_params = {
                    PARAMETER_PYTHON_CONNECTOR_QUERY_RESULT_FORMAT: "JSON"
                }
            else:
                result_format_val = statement_params.get(
                    PARAMETER_PYTHON_CONNECTOR_QUERY_RESULT_FORMAT
                )
                if str(result_format_val).upper() == "ARROW":
                    self.check_can_use_arrow_resultset()
                elif result_format_val is None:
                    statement_params[PARAMETER_PYTHON_CONNECTOR_QUERY_RESULT_FORMAT] = (
                        "JSON"
                    )

        self._sequence_counter = self._connection._next_sequence_counter()
        self._request_id = uuid.uuid4()

        logger.debug(f"Request id: {self._request_id}")

        logger.debug("running query [%s]", self._format_query_for_log(query))
        if _is_put_get is not None:
            # if told the query is PUT or GET, use the information
            self._is_file_transfer = _is_put_get
        else:
            # or detect it.
            self._is_file_transfer = get_file_transfer_type(query) is not None
        logger.debug("is_file_transfer: %s", self._is_file_transfer is not None)

        real_timeout = (
            timeout if timeout and timeout > 0 else self._connection.network_timeout
        )

        if real_timeout is not None:
            self._timebomb = _TrackedQueryCancellationTimer(
                real_timeout, self.__cancel_query, [query]
            )
            self._timebomb.start()
            logger.debug("started timebomb in %ss", real_timeout)
        else:
            self._timebomb = None

        original_sigint = signal.getsignal(signal.SIGINT)

        def interrupt_handler(*_):  # pragma: no cover
            try:
                signal.signal(signal.SIGINT, exit_handler)
            except (ValueError, TypeError):
                # ignore failures
                pass
            try:
                if self._timebomb is not None:
                    self._timebomb.cancel()
                    logger.debug("cancelled timebomb in finally")
                    self._timebomb = None
                self.__cancel_query(query)
            finally:
                if original_sigint:
                    try:
                        signal.signal(signal.SIGINT, original_sigint)
                    except (ValueError, TypeError):
                        # ignore failures
                        pass
            raise KeyboardInterrupt

        try:
            if not original_sigint == exit_handler:
                signal.signal(signal.SIGINT, interrupt_handler)
        except ValueError:  # pragma: no cover
            logger.debug(
                "Failed to set SIGINT handler. " "Not in main thread. Ignored..."
            )
        ret: dict[str, Any] = {"data": {}}
        try:
            ret = self._connection.cmd_query(
                query,
                self._sequence_counter,
                self._request_id,
                binding_params=binding_params,
                binding_stage=binding_stage,
                is_file_transfer=bool(self._is_file_transfer),
                statement_params=statement_params,
                is_internal=is_internal,
                describe_only=describe_only,
                _no_results=_no_results,
                _no_retry=_no_retry,
                timeout=real_timeout,
                dataframe_ast=dataframe_ast,
            )
        finally:
            try:
                if original_sigint:
                    signal.signal(signal.SIGINT, original_sigint)
            except (ValueError, TypeError):  # pragma: no cover
                logger.debug(
                    "Failed to reset SIGINT handler. Not in main " "thread. Ignored..."
                )
            if self._timebomb is not None:
                self._timebomb.cancel()
                logger.debug("cancelled timebomb in finally")

        if "data" in ret and "parameters" in ret["data"]:
            parameters = ret["data"].get("parameters", list())
            # Set session parameters for cursor object
            for kv in parameters:
                if "TIMESTAMP_OUTPUT_FORMAT" in kv["name"]:
                    self._timestamp_output_format = kv["value"]
                elif "TIMESTAMP_NTZ_OUTPUT_FORMAT" in kv["name"]:
                    self._timestamp_ntz_output_format = kv["value"]
                elif "TIMESTAMP_LTZ_OUTPUT_FORMAT" in kv["name"]:
                    self._timestamp_ltz_output_format = kv["value"]
                elif "TIMESTAMP_TZ_OUTPUT_FORMAT" in kv["name"]:
                    self._timestamp_tz_output_format = kv["value"]
                elif "DATE_OUTPUT_FORMAT" in kv["name"]:
                    self._date_output_format = kv["value"]
                elif "TIME_OUTPUT_FORMAT" in kv["name"]:
                    self._time_output_format = kv["value"]
                elif "TIMEZONE" in kv["name"]:
                    self._timezone = kv["value"]
                elif "BINARY_OUTPUT_FORMAT" in kv["name"]:
                    self._binary_output_format = kv["value"]
            # Set session parameters for connection object
            self._connection._update_parameters(
                {p["name"]: p["value"] for p in parameters}
            )

        self.query = query
        self._sequence_counter = -1
        return ret

    def _preprocess_pyformat_query(
        self,
        command: str,
        params: Sequence[Any] | dict[Any, Any] | None = None,
    ) -> str:
        # pyformat/format paramstyle
        # client side binding
        processed_params = self._connection._process_params_pyformat(params, self)
        # SNOW-513061 collect telemetry for empty sequence usage before we make the breaking change announcement
        if params is not None and len(params) == 0:
            self._log_telemetry_job_data(
                TelemetryField.EMPTY_SEQ_INTERPOLATION,
                (
                    TelemetryData.TRUE
                    if self.connection._interpolate_empty_sequences
                    else TelemetryData.FALSE
                ),
            )
        if logger.getEffectiveLevel() <= logging.DEBUG:
            logger.debug(
                f"binding: [{self._format_query_for_log(command)}] "
                f"with input=[{params}], "
                f"processed=[{processed_params}]",
            )
        if (
            self.connection._interpolate_empty_sequences
            and processed_params is not None
        ) or (
            not self.connection._interpolate_empty_sequences
            and len(processed_params) > 0
        ):
            query = command % processed_params
        else:
            query = command
        return query

    @overload
    def execute(
        self,
        command: str,
        params: Sequence[Any] | dict[Any, Any] | None = None,
        _bind_stage: str | None = None,
        timeout: int | None = None,
        _exec_async: bool = False,
        _no_retry: bool = False,
        _do_reset: bool = True,
        _put_callback: SnowflakeProgressPercentage = None,
        _put_azure_callback: SnowflakeProgressPercentage = None,
        _put_callback_output_stream: IO[str] = sys.stdout,
        _get_callback: SnowflakeProgressPercentage = None,
        _get_azure_callback: SnowflakeProgressPercentage = None,
        _get_callback_output_stream: IO[str] = sys.stdout,
        _show_progress_bar: bool = True,
        _statement_params: dict[str, str] | None = None,
        _is_internal: bool = False,
        _describe_only: bool = False,
        _no_results: Literal[False] = False,
        _is_put_get: bool | None = None,
        _raise_put_get_error: bool = True,
        _force_put_overwrite: bool = False,
        _skip_upload_on_content_match: bool = False,
        file_stream: IO[bytes] | None = None,
        num_statements: int | None = None,
        _dataframe_ast: str | None = None,
    ) -> Self | None: ...

    @overload
    def execute(
        self,
        command: str,
        params: Sequence[Any] | dict[Any, Any] | None = None,
        _bind_stage: str | None = None,
        timeout: int | None = None,
        _exec_async: bool = False,
        _no_retry: bool = False,
        _do_reset: bool = True,
        _put_callback: SnowflakeProgressPercentage = None,
        _put_azure_callback: SnowflakeProgressPercentage = None,
        _put_callback_output_stream: IO[str] = sys.stdout,
        _get_callback: SnowflakeProgressPercentage = None,
        _get_azure_callback: SnowflakeProgressPercentage = None,
        _get_callback_output_stream: IO[str] = sys.stdout,
        _show_progress_bar: bool = True,
        _statement_params: dict[str, str] | None = None,
        _is_internal: bool = False,
        _describe_only: bool = False,
        _no_results: Literal[True] = True,
        _is_put_get: bool | None = None,
        _raise_put_get_error: bool = True,
        _force_put_overwrite: bool = False,
        _skip_upload_on_content_match: bool = False,
        file_stream: IO[bytes] | None = None,
        num_statements: int | None = None,
        _dataframe_ast: str | None = None,
    ) -> dict[str, Any] | None: ...

    def execute(
        self,
        command: str,
        params: Sequence[Any] | dict[Any, Any] | None = None,
        _bind_stage: str | None = None,
        timeout: int | None = None,
        _exec_async: bool = False,
        _no_retry: bool = False,
        _do_reset: bool = True,
        _put_callback: SnowflakeProgressPercentage = None,
        _put_azure_callback: SnowflakeProgressPercentage = None,
        _put_callback_output_stream: IO[str] = sys.stdout,
        _get_callback: SnowflakeProgressPercentage = None,
        _get_azure_callback: SnowflakeProgressPercentage = None,
        _get_callback_output_stream: IO[str] = sys.stdout,
        _show_progress_bar: bool = True,
        _statement_params: dict[str, str] | None = None,
        _is_internal: bool = False,
        _describe_only: bool = False,
        _no_results: bool = False,
        _is_put_get: bool | None = None,
        _raise_put_get_error: bool = True,
        _force_put_overwrite: bool = False,
        _skip_upload_on_content_match: bool = False,
        file_stream: IO[bytes] | None = None,
        num_statements: int | None = None,
        _dataframe_ast: str | None = None,
    ) -> Self | dict[str, Any] | None:
        """Executes a command/query.

        Args:
            command: The SQL command to be executed.
            params: Parameters to be bound into the SQL statement.
            _bind_stage: Path in temporary stage where binding parameters are uploaded as CSV files.
            timeout: Number of seconds after which to abort the query.
            _exec_async: Whether to execute this query asynchronously.
            _no_retry: Whether or not to retry on known errors.
            _do_reset: Whether or not the result set needs to be reset before executing query.
            _put_callback: Function to which GET command should call back to.
            _put_azure_callback: Function to which an Azure GET command should call back to.
            _put_callback_output_stream: The output stream a PUT command's callback should report on.
            _get_callback: Function to which GET command should call back to.
            _get_azure_callback: Function to which an Azure GET command should call back to.
            _get_callback_output_stream: The output stream a GET command's callback should report on.
            _show_progress_bar: Whether or not to show progress bar.
            _statement_params: Extra information that should be sent to Snowflake with query. This dict will not be
                modified by the connector.
            _is_internal: This flag indicates whether the query is issued internally by the connector.
            _describe_only: If true, the query will not be executed but return the schema/description of this query.
            _no_results: This flag tells the back-end to not return the result, just fire the query and return the
                response returned by Snowflake's server.
            _use_ijson: This flag doesn't do anything as ijson support has ended.
            _is_put_get: Force decision of this SQL query being a PUT, or GET command. This is detected otherwise.
            _raise_put_get_error: Whether to raise PUT and GET errors.
            _force_put_overwrite: If the SQL query is a PUT, then this flag can force overwriting of an already
                existing file on stage.
            _skip_upload_on_content_match: If the SQL query is a PUT with overwrite enabled, then this flag will skip upload
                if the file contents match to ease concurrent uploads.
            file_stream: File-like object to be uploaded with PUT
            num_statements: Query level parameter submitted in _statement_params constraining exact number of
            statements being submitted (or 0 if submitting an uncounted number) when using a multi-statement query.
            _dataframe_ast: Base64-encoded dataframe request abstract syntax tree.

        Returns:
            The cursor itself, or None if some error happened, or the response returned
            by Snowflake if the _no_results flag is on.
        """
        if _exec_async:
            _no_results = True
        logger.debug("executing SQL/command")
        if self.is_closed():
            Error.errorhandler_wrapper(
                self.connection,
                self,
                InterfaceError,
                {"msg": "Cursor is closed in execute.", "errno": ER_CURSOR_IS_CLOSED},
            )

        if _do_reset:
            self.reset()
        command = command.strip(" \t\n\r") if command else None
        if not command:
            logger.warning("execute: no query is given to execute")
            return None
        logger.debug("query: [%s]", self._format_query_for_log(command))

        _statement_params = _statement_params or dict()
        # If we need to add another parameter, please consider introducing a dict for all extra params
        # See discussion in https://github.com/snowflakedb/snowflake-connector-python/pull/1524#discussion_r1174061775
        if num_statements is not None:
            _statement_params = {
                **_statement_params,
                "MULTI_STATEMENT_COUNT": num_statements,
            }

        kwargs: dict[str, Any] = {
            "timeout": timeout,
            "statement_params": _statement_params,
            "is_internal": _is_internal,
            "describe_only": _describe_only,
            "_no_results": _no_results,
            "_is_put_get": _is_put_get,
            "_no_retry": _no_retry,
            "dataframe_ast": _dataframe_ast,
        }

        if self._connection.is_pyformat:
            query = self._preprocess_pyformat_query(command, params)
        else:
            # qmark and numeric paramstyle
            query = command
            if _bind_stage:
                kwargs["binding_stage"] = _bind_stage
            else:
                if params is not None and not isinstance(params, (list, tuple)):
                    errorvalue = {
                        "msg": f"Binding parameters must be a list: {params}",
                        "errno": ER_FAILED_PROCESSING_PYFORMAT,
                    }
                    Error.errorhandler_wrapper(
                        self.connection, self, ProgrammingError, errorvalue
                    )

                kwargs["binding_params"] = self._connection._process_params_qmarks(
                    params, self
                )

        m = DESC_TABLE_RE.match(query)
        if m:
            query1 = f"describe table {m.group(1)}"
            logger.debug(
                "query was rewritten: org=%s, new=%s",
                " ".join(line.strip() for line in query.split("\n")),
                query1,
            )
            query = query1

        ret = self._execute_helper(query, **kwargs)
        self._sfqid = (
            ret["data"]["queryId"]
            if "data" in ret and "queryId" in ret["data"]
            else None
        )
        logger.debug(f"sfqid: {self.sfqid}")
        self._sqlstate = (
            ret["data"]["sqlState"]
            if "data" in ret and "sqlState" in ret["data"]
            else None
        )
        logger.debug("query execution done")

        self._first_chunk_time = get_time_millis()

        # if server gives a send time, log the time it took to arrive
        if "data" in ret and "sendResultTime" in ret["data"]:
            time_consume_first_result = (
                self._first_chunk_time - ret["data"]["sendResultTime"]
            )
            self._log_telemetry_job_data(
                TelemetryField.TIME_CONSUME_FIRST_RESULT, time_consume_first_result
            )

        if ret["success"]:
            logger.debug("SUCCESS")
            data = ret["data"]

            for m in self.ALTER_SESSION_RE.finditer(query):
                # session parameters
                param = m.group(1).upper()
                value = m.group(2)
                self._connection.converter.set_parameter(param, value)

            if "resultIds" in data:
                self._init_multi_statement_results(data)
                return self
            else:
                self.multi_statement_savedIds = []

            self._is_file_transfer = "command" in data and data["command"] in (
                "UPLOAD",
                "DOWNLOAD",
            )
            logger.debug("PUT OR GET: %s", self.is_file_transfer)
            if self.is_file_transfer:
                from .file_transfer_agent import SnowflakeFileTransferAgent

                # Decide whether to use the old, or new code path
                sf_file_transfer_agent = SnowflakeFileTransferAgent(
                    self,
                    query,
                    ret,
                    put_callback=_put_callback,
                    put_azure_callback=_put_azure_callback,
                    put_callback_output_stream=_put_callback_output_stream,
                    get_callback=_get_callback,
                    get_azure_callback=_get_azure_callback,
                    get_callback_output_stream=_get_callback_output_stream,
                    show_progress_bar=_show_progress_bar,
                    raise_put_get_error=_raise_put_get_error,
                    force_put_overwrite=_force_put_overwrite
                    or data.get("overwrite", False),
                    skip_upload_on_content_match=_skip_upload_on_content_match,
                    source_from_stream=file_stream,
                    multipart_threshold=data.get("threshold"),
                    use_s3_regional_url=self._connection.enable_stage_s3_privatelink_for_us_east_1,
                )
                sf_file_transfer_agent.execute()
                data = sf_file_transfer_agent.result()
                self._total_rowcount = len(data["rowset"]) if "rowset" in data else -1

            if _exec_async:
                self.connection._async_sfqids[self._sfqid] = None
            if _no_results:
                self._total_rowcount = (
                    ret["data"]["total"]
                    if "data" in ret and "total" in ret["data"]
                    else -1
                )
                return data
            self._init_result_and_meta(data)
        else:
            self._total_rowcount = (
                ret["data"]["total"] if "data" in ret and "total" in ret["data"] else -1
            )
            logger.debug(ret)
            err = ret["message"]
            code = ret.get("code", -1)
            if self._timebomb and self._timebomb.executed:
                err = (
                    f"SQL execution was cancelled by the client due to a timeout. "
                    f"Error message received from the server: {err}"
                )
            if "data" in ret:
                err += ret["data"].get("errorMessage", "")
            errvalue = {
                "msg": err,
                "errno": int(code),
                "sqlstate": self._sqlstate,
                "sfqid": self._sfqid,
                "query": query,
            }
            is_integrity_error = (
                code == "100072"
            )  # NULL result in a non-nullable column
            error_class = IntegrityError if is_integrity_error else ProgrammingError
            Error.errorhandler_wrapper(self.connection, self, error_class, errvalue)
        return self

    def execute_async(self, *args: Any, **kwargs: Any) -> dict[str, Any]:
        """Convenience function to execute a query without waiting for results (asynchronously).

        This function takes the same arguments as execute, please refer to that function
        for documentation. Please note that PUT and GET statements are not supported by this method.
        """
        kwargs["_exec_async"] = True
        return self.execute(*args, **kwargs)

    def describe(self, *args: Any, **kwargs: Any) -> list[ResultMetadata]:
        """Obtain the schema of the result without executing the query.

        This function takes the same arguments as execute, please refer to that function
        for documentation.

        Returns:
            The schema of the result.
        """
        kwargs["_describe_only"] = kwargs["_is_internal"] = True
        self.execute(*args, **kwargs)

        if self._description is None:
            return None
        return [meta._to_result_metadata_v1() for meta in self._description]

    def _describe_internal(self, *args: Any, **kwargs: Any) -> list[ResultMetadataV2]:
        """Obtain the schema of the result without executing the query.

        This function takes the same arguments as execute, please refer to that function
        for documentation.

        This function is for internal use only

        Returns:
            The schema of the result, in the new result metadata format.
        """
        kwargs["_describe_only"] = kwargs["_is_internal"] = True
        self.execute(*args, **kwargs)
        return self._description

    def _format_query_for_log(self, query: str) -> str:
        return self._connection._format_query_for_log(query)

    def _is_dml(self, data: dict[Any, Any]) -> bool:
        return (
            "statementTypeId" in data
            and int(data["statementTypeId"]) in STATEMENT_TYPE_ID_DML_SET
        )

    def _init_result_and_meta(self, data: dict[Any, Any]) -> None:
        is_dml = self._is_dml(data)
        self._query_result_format = data.get("queryResultFormat", "json")
        logger.debug("Query result format: %s", self._query_result_format)

        if self._total_rowcount == -1 and not is_dml and data.get("total") is not None:
            self._total_rowcount = data["total"]

        self._description: list[ResultMetadataV2] = [
            ResultMetadataV2.from_column(col) for col in data["rowtype"]
        ]

        result_chunks = create_batches_from_response(
            self, self._query_result_format, data, self._description
        )

        if not (is_dml or self.is_file_transfer):
            logger.info(
                "Number of results in first chunk: %s", result_chunks[0].rowcount
            )

        self._result_set = ResultSet(
            self,
            result_chunks,
            self._connection.client_prefetch_threads,
        )
        self._rownumber = -1
        self._result_state = ResultState.VALID

        # don't update the row count when the result is returned from `describe` method
        if is_dml and "rowset" in data and len(data["rowset"]) > 0:
            updated_rows = 0
            for idx, desc in enumerate(self._description):
                if desc.name in (
                    "number of rows updated",
                    "number of multi-joined rows updated",
                    "number of rows deleted",
                ) or desc.name.startswith("number of rows inserted"):
                    updated_rows += int(data["rowset"][0][idx])
            if self._total_rowcount == -1:
                self._total_rowcount = updated_rows
            else:
                self._total_rowcount += updated_rows

    def _init_multi_statement_results(self, data: dict) -> None:
        self._log_telemetry_job_data(TelemetryField.MULTI_STATEMENT, TelemetryData.TRUE)
        self.multi_statement_savedIds = data["resultIds"].split(",")
        self._multi_statement_resultIds = collections.deque(
            self.multi_statement_savedIds
        )
        if self._is_file_transfer:
            Error.errorhandler_wrapper(
                self.connection,
                self,
                ProgrammingError,
                {
                    "msg": "PUT/GET commands are not supported for multi-statement queries and cannot be executed.",
                    "errno": ER_INVALID_VALUE,
                },
            )
        self.nextset()

    def check_can_use_arrow_resultset(self) -> None:
        global CAN_USE_ARROW_RESULT_FORMAT

        if not CAN_USE_ARROW_RESULT_FORMAT:
            if self._connection.application == "SnowSQL":
                msg = "Currently SnowSQL doesn't support the result set in Apache Arrow format."
                errno = ER_NO_PYARROW_SNOWSQL
            else:
                msg = "The result set in Apache Arrow format is not supported for the platform."
                errno = ER_NO_ARROW_RESULT

            Error.errorhandler_wrapper(
                self.connection,
                self,
                ProgrammingError,
                {
                    "msg": msg,
                    "errno": errno,
                },
            )

    def check_can_use_pandas(self) -> None:
        if not installed_pandas:
            msg = (
                "Optional dependency: 'pandas' is not installed, please see the following link for install "
                "instructions: https://docs.snowflake.com/en/user-guide/python-connector-pandas.html#installation"
            )
            errno = ER_NO_PYARROW

            Error.errorhandler_wrapper(
                self.connection,
                self,
                ProgrammingError,
                {
                    "msg": msg,
                    "errno": errno,
                },
            )

    def query_result(self, qid: str) -> SnowflakeCursor:
        """Query the result of a previously executed query."""
        url = f"/queries/{qid}/result"
        ret = self._connection.rest.request(url=url, method="get")
        self._sfqid = (
            ret["data"]["queryId"]
            if "data" in ret and "queryId" in ret["data"]
            else None
        )
        self._sqlstate = (
            ret["data"]["sqlState"]
            if "data" in ret and "sqlState" in ret["data"]
            else None
        )
        logger.debug("sfqid=%s", self._sfqid)

        if ret.get("success"):
            data = ret.get("data")
            self._init_result_and_meta(data)
        else:
            logger.info("failed")
            logger.debug(ret)
            err = ret["message"]
            code = ret.get("code", -1)
            if "data" in ret:
                err += ret["data"].get("errorMessage", "")
            errvalue = {
                "msg": err,
                "errno": int(code),
                "sqlstate": self._sqlstate,
                "sfqid": self._sfqid,
            }
            Error.errorhandler_wrapper(
                self.connection, self, ProgrammingError, errvalue
            )
        return self

    def fetch_arrow_batches(self) -> Iterator[Table]:
        self.check_can_use_arrow_resultset()
        if self._prefetch_hook is not None:
            self._prefetch_hook()
        if self._query_result_format != "arrow":
            raise NotSupportedError
        self._log_telemetry_job_data(
            TelemetryField.ARROW_FETCH_BATCHES, TelemetryData.TRUE
        )
        return self._result_set._fetch_arrow_batches()

    @overload
    def fetch_arrow_all(self, force_return_table: Literal[False]) -> Table | None: ...

    @overload
    def fetch_arrow_all(self, force_return_table: Literal[True]) -> Table: ...

    def fetch_arrow_all(self, force_return_table: bool = False) -> Table | None:
        """
        Args:
            force_return_table: Set to True so that when the query returns zero rows,
                an empty pyarrow table will be returned with schema using the highest bit length for each column.
                Default value is False in which case None is returned in case of zero rows.
        """
        self.check_can_use_arrow_resultset()

        if self._prefetch_hook is not None:
            self._prefetch_hook()
        if self._query_result_format != "arrow":
            raise NotSupportedError
        self._log_telemetry_job_data(TelemetryField.ARROW_FETCH_ALL, TelemetryData.TRUE)
        return self._result_set._fetch_arrow_all(force_return_table=force_return_table)

    def fetch_pandas_batches(self, **kwargs: Any) -> Iterator[DataFrame]:
        """Fetches a single Arrow Table."""
        self.check_can_use_pandas()
        if self._prefetch_hook is not None:
            self._prefetch_hook()
        if self._query_result_format != "arrow":
            raise NotSupportedError
        self._log_telemetry_job_data(
            TelemetryField.PANDAS_FETCH_BATCHES, TelemetryData.TRUE
        )
        return self._result_set._fetch_pandas_batches(**kwargs)

    def fetch_pandas_all(self, **kwargs: Any) -> DataFrame:
        """
        Fetch Pandas dataframes in batches, where 'batch' refers to Snowflake Chunk.

        Returns:
            A pandas dataframe containing the cursors query results.

        Note:
            Timestamp types that are nested in structured types may not be accurately represented
            due to a limitation in the pyarrow to pandas conversion.
            See here for more information: https://github.com/apache/arrow/issues/4116
        """
        self.check_can_use_pandas()
        if self._prefetch_hook is not None:
            self._prefetch_hook()
        if self._query_result_format != "arrow":
            raise NotSupportedError
        self._log_telemetry_job_data(
            TelemetryField.PANDAS_FETCH_ALL, TelemetryData.TRUE
        )
        return self._result_set._fetch_pandas_all(**kwargs)

    def abort_query(self, qid: str) -> bool:
        url = f"/queries/{qid}/abort-request"
        ret = self._connection.rest.request(url=url, method="post")
        return ret.get("success")

    def executemany(
        self,
        command: str,
        seqparams: Sequence[Any] | dict[str, Any],
        **kwargs: Any,
    ) -> SnowflakeCursor:
        """Executes a command/query with the given set of parameters sequentially."""
        logger.debug("executing many SQLs/commands")
        command = command.strip(" \t\n\r") if command else None

        if not seqparams:
            logger.warning(
                "No parameters provided to executemany, returning without doing anything."
            )
            return self

        if self.INSERT_SQL_RE.match(command) and (
            "num_statements" not in kwargs or kwargs.get("num_statements") == 1
        ):
            if self._connection.is_pyformat:
                # TODO(SNOW-940692) - utilize multi-statement instead of rewriting the query and
                #  accumulate results to mock the result from a single insert statement as formatted below
                logger.debug("rewriting INSERT query")
                command_wo_comments = re.sub(self.COMMENT_SQL_RE, "", command)
                m = self.INSERT_SQL_VALUES_RE.match(command_wo_comments)
                if not m:
                    Error.errorhandler_wrapper(
                        self.connection,
                        self,
                        InterfaceError,
                        {
                            "msg": "Failed to rewrite multi-row insert",
                            "errno": ER_FAILED_TO_REWRITE_MULTI_ROW_INSERT,
                        },
                    )

                fmt = m.group(1)
                values = []
                for param in seqparams:
                    logger.debug(f"parameter: {param}")
                    values.append(
                        fmt % self._connection._process_params_pyformat(param, self)
                    )
                command = command.replace(fmt, ",".join(values), 1)
                self.execute(command, **kwargs)
                return self
            else:
                logger.debug("bulk insert")
                # sanity check
                row_size = len(seqparams[0])
                for row in seqparams:
                    if len(row) != row_size:
                        error_value = {
                            "msg": f"Bulk data size don't match. expected: {row_size}, "
                            f"got: {len(row)}, command: {command}",
                            "errno": ER_INVALID_VALUE,
                        }
                        Error.errorhandler_wrapper(
                            self.connection, self, InterfaceError, error_value
                        )
                        return self
                bind_size = len(seqparams) * row_size
                bind_stage = None
                if (
                    bind_size
                    > self.connection._session_parameters[
                        "CLIENT_STAGE_ARRAY_BINDING_THRESHOLD"
                    ]
                    > 0
                ):
                    # bind stage optimization
                    try:
                        rows = self.connection._write_params_to_byte_rows(seqparams)
                        bind_uploader = BindUploadAgent(self, rows)
                        bind_uploader.upload()
                        bind_stage = bind_uploader.stage_path
                    except BindUploadError:
                        logger.debug(
                            "Failed to upload binds to stage, sending binds to "
                            "Snowflake instead."
                        )
                binding_param = (
                    None if bind_stage else list(map(list, zip(*seqparams)))
                )  # transpose
                self.execute(
                    command, params=binding_param, _bind_stage=bind_stage, **kwargs
                )
                return self

        self.reset()
        if "num_statements" not in kwargs:
            # fall back to old driver behavior when the user does not provide the parameter to enable
            #  multi-statement optimizations for executemany
            for param in seqparams:
                self.execute(command, params=param, _do_reset=False, **kwargs)
        else:
            if re.search(";/s*$", command) is None:
                command = command + "; "
            if self._connection.is_pyformat:
                processed_queries = [
                    self._preprocess_pyformat_query(command, params)
                    for params in seqparams
                ]
                query = "".join(processed_queries)
                params = None
            else:
                query = command * len(seqparams)
                params = [param for parameters in seqparams for param in parameters]

            kwargs["num_statements"]: int = kwargs.get("num_statements") * len(
                seqparams
            )

            self.execute(query, params, _do_reset=False, **kwargs)

        return self

    def fetchone(self) -> dict | tuple | None:
        """Fetches one row."""
        if self._prefetch_hook is not None:
            self._prefetch_hook()
        if self._result is None and self._result_set is not None:
            self._result = iter(self._result_set)
            self._result_state = ResultState.VALID

        try:
            _next = next(self._result, None)
            if isinstance(_next, Exception):
                Error.errorhandler_wrapper_from_ready_exception(
                    self._connection,
                    self,
                    _next,
                )
            if _next is not None:
                self._rownumber += 1
            return _next
        except TypeError as err:
            if self._result_state == ResultState.DEFAULT:
                raise err
            else:
                return None

    def fetchmany(self, size: int | None = None) -> list[tuple] | list[dict]:
        """Fetches the number of specified rows."""
        if size is None:
            size = self.arraysize

        if size < 0:
            errorvalue = {
                "msg": (
                    "The number of rows is not zero or " "positive number: {}"
                ).format(size),
                "errno": ER_NOT_POSITIVE_SIZE,
            }
            Error.errorhandler_wrapper(
                self.connection, self, ProgrammingError, errorvalue
            )
        ret = []
        while size > 0:
            row = self.fetchone()
            if row is None:
                break
            ret.append(row)
            if size is not None:
                size -= 1

        return ret

    def fetchall(self) -> list[tuple] | list[dict]:
        """Fetches all of the results."""
        ret = []
        while True:
            row = self.fetchone()
            if row is None:
                break
            ret.append(row)
        return ret

    def nextset(self) -> SnowflakeCursor | None:
        """
        Fetches the next set of results if the previously executed query was multi-statement so that subsequent calls
        to any of the fetch*() methods will return rows from the next query's set of results. Returns None if no more
        query results are available.
        """
        if self._prefetch_hook is not None:
            self._prefetch_hook()
        self.reset()
        if self._multi_statement_resultIds:
            self.query_result(self._multi_statement_resultIds[0])
            logger.info(
                f"Retrieved results for query ID: {self._multi_statement_resultIds.popleft()}"
            )
            return self

        return None

    def setinputsizes(self, _) -> None:
        """Not supported."""
        logger.debug("nop")

    def setoutputsize(self, _, column=None) -> None:
        """Not supported."""
        del column
        logger.debug("nop")

    def scroll(self, value, mode: str = "relative") -> None:
        Error.errorhandler_wrapper(
            self.connection,
            self,
            NotSupportedError,
            {
                "msg": "scroll is not supported.",
                "errno": ER_UNSUPPORTED_METHOD,
                "sqlstate": SQLSTATE_FEATURE_NOT_SUPPORTED,
            },
        )

    def reset(self, closing: bool = False) -> None:
        """Resets the result set."""
        # SNOW-647539: Do not erase the rowcount
        # information when closing the cursor
        if not closing:
            self._total_rowcount = -1
        if self._result_state != ResultState.DEFAULT:
            self._result_state = ResultState.RESET
        if self._result is not None:
            self._result = None
        if self._inner_cursor is not None:
            self._inner_cursor.reset(closing=closing)
            self._result = None
            self._inner_cursor = None
        self._prefetch_hook = None
        if not self.connection._reuse_results:
            self._result_set = None

    def __iter__(self) -> Iterator[dict] | Iterator[tuple]:
        """Iteration over the result set."""
        while True:
            _next = self.fetchone()
            if _next is None:
                break
            yield _next

    def __cancel_query(self, query) -> None:
        if self._sequence_counter >= 0 and not self.is_closed():
            logger.debug("canceled. %s, request_id: %s", query, self._request_id)
            with self._lock_canceling:
                self._connection._cancel_query(query, self._request_id)

    def _log_telemetry_job_data(
        self, telemetry_field: TelemetryField, value: Any
    ) -> None:
        """Builds an instance of TelemetryData with the given field and logs it."""
        ts = get_time_millis()
        try:
            self._connection._log_telemetry(
                TelemetryData.from_telemetry_data_dict(
                    from_dict={
                        TelemetryField.KEY_TYPE.value: telemetry_field.value,
                        TelemetryField.KEY_SFQID.value: self._sfqid,
                        TelemetryField.KEY_VALUE.value: value,
                    },
                    timestamp=ts,
                    connection=self._connection,
                )
            )
        except AttributeError:
            logger.warning(
                "Cursor failed to log to telemetry. Connection object may be None.",
                exc_info=True,
            )

    def __enter__(self) -> Self:
        """Context manager."""
        return self

    def __exit__(
        self,
        exc_type: type[BaseException] | None,
        exc_val: BaseException | None,
        exc_tb: TracebackType | None,
    ) -> None:
        """Context manager with commit or rollback."""
        self.close()

    def get_results_from_sfqid(self, sfqid: str) -> None:
        """Gets the results from previously ran query. This methods differs from ``SnowflakeCursor.query_result``
        in that it monitors the ``sfqid`` until it is no longer running, and then retrieves the results.
        """

        def wait_until_ready() -> None:
            """Makes sure query has finished executing and once it has retrieves results."""
            no_data_counter = 0
            retry_pattern_pos = 0
            while True:
                status, status_resp = self.connection._get_query_status(sfqid)
                self.connection._cache_query_status(sfqid, status)
                if not self.connection.is_still_running(status):
                    break
                if status == QueryStatus.NO_DATA:  # pragma: no cover
                    no_data_counter += 1
                    if no_data_counter > ASYNC_NO_DATA_MAX_RETRY:
                        raise DatabaseError(
                            "Cannot retrieve data on the status of this query. No information returned "
                            "from server for query '{}'"
                        )
                time.sleep(
                    0.5 * ASYNC_RETRY_PATTERN[retry_pattern_pos]
                )  # Same wait as JDBC
                # If we can advance in ASYNC_RETRY_PATTERN then do so
                if retry_pattern_pos < (len(ASYNC_RETRY_PATTERN) - 1):
                    retry_pattern_pos += 1
            if status != QueryStatus.SUCCESS:
                logger.info(f"Status of query '{sfqid}' is {status.name}")
                self.connection._process_error_query_status(
                    sfqid,
                    status_resp,
                    error_message=f"Status of query '{sfqid}' is {status.name}, results are unavailable",
                    error_cls=DatabaseError,
                )
            self._inner_cursor.execute(f"select * from table(result_scan('{sfqid}'))")
            self._result = self._inner_cursor._result
            self._query_result_format = self._inner_cursor._query_result_format
            self._total_rowcount = self._inner_cursor._total_rowcount
            self._description = self._inner_cursor._description
            self._result_set = self._inner_cursor._result_set
            self._result_state = ResultState.VALID
            self._rownumber = 0
            # Unset this function, so that we don't block anymore
            self._prefetch_hook = None

            if (
                self._inner_cursor._total_rowcount == 1
                and self._inner_cursor.fetchall()
                == [("Multiple statements executed successfully.",)]
            ):
                url = f"/queries/{sfqid}/result"
                ret = self._connection.rest.request(url=url, method="get")
                if "data" in ret and "resultIds" in ret["data"]:
                    self._init_multi_statement_results(ret["data"])

        self.connection.get_query_status_throw_if_error(
            sfqid
        )  # Trigger an exception if query failed
        klass = self.__class__
        self._inner_cursor = klass(self.connection)
        self._sfqid = sfqid
        self._prefetch_hook = wait_until_ready

    def get_result_batches(self) -> list[ResultBatch] | None:
        """Get the previously executed query's ``ResultBatch`` s if available.

        If they are unavailable, in case nothing has been executed yet None will
        be returned.

        For a detailed description of ``ResultBatch`` s please see the docstring of:
        ``snowflake.connector.result_batches.ResultBatch``
        """
        if self._result_set is None:
            return None
        self._log_telemetry_job_data(
            TelemetryField.GET_PARTITIONS_USED, TelemetryData.TRUE
        )
        return self._result_set.batches


class DictCursor(SnowflakeCursor):
    """Cursor returning results in a dictionary."""

    def __init__(self, connection) -> None:
        super().__init__(
            connection,
            use_dict_result=True,
        )


def __getattr__(name):
    if name == "NanoarrowUsage":
        warnings.warn(
            "snowflake.connector.cursor.NanoarrowUsage has been deprecated and will be removed in the future, "
            "please stop using the class.",
            DeprecationWarning,
            stacklevel=2,
        )
        return _NanoarrowUsage
    if name == "NANOARROW_USAGE":
        if "NANOARROW_USAGE" in os.environ:
            warnings.warn(
                "Environment variable NANOARROW_USAGE has been deprecated and will be removed in the future, "
                "please stop setting the variable.",
                DeprecationWarning,
                stacklevel=2,
            )

        warnings.warn(
            "snowflake.connector.cursor.NANOARROW_USAGE has been deprecated and will be removed in the future, "
            "please stop using the variable.",
            DeprecationWarning,
            stacklevel=2,
        )
        return None
    raise AttributeError(f"module {__name__!r} has no attribute {name!r}")

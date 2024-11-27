#!/usr/bin/env python
#
# Copyright (c) 2012-2023 Snowflake Computing Inc. All rights reserved.
#

from __future__ import annotations

from collections import defaultdict
from enum import Enum, auto, unique
from typing import TYPE_CHECKING, Any, Callable, DefaultDict, NamedTuple

from .options import pyarrow as pa
from .sf_dirs import _resolve_platform_dirs

if TYPE_CHECKING:
    from pyarrow import DataType

    from .cursor import ResultMetadataV2

# Snowflake's central platform dependent directories, if the folder
# ~/.snowflake/ (customizable by the environment variable SNOWFLAKE_HOME) exists
# we use that folder for everything. Otherwise, we fall back to platformdirs
# defaults. Please see comments in sf_dir.py for more information.
DIRS = _resolve_platform_dirs()

# Snowflake's configuration files. By default, platformdirs will resolve
# them to these places depending on OS:
#   * Linux: `~/.config/snowflake/filename` but can be updated with XDG vars
#   * Windows: `%USERPROFILE%\AppData\Local\snowflake\filename`
#   * Mac: `~/Library/Application Support/snowflake/filename`
CONNECTIONS_FILE = DIRS.user_config_path / "connections.toml"
CONFIG_FILE = DIRS.user_config_path / "config.toml"

DBAPI_TYPE_STRING = 0
DBAPI_TYPE_BINARY = 1
DBAPI_TYPE_NUMBER = 2
DBAPI_TYPE_TIMESTAMP = 3

_DEFAULT_HOSTNAME_TLD = "com"
_CHINA_HOSTNAME_TLD = "cn"
_TOP_LEVEL_DOMAIN_REGEX = r"\.[a-zA-Z]{1,63}$"
_SNOWFLAKE_HOST_SUFFIX_REGEX = r"snowflakecomputing(\.[a-zA-Z]{1,63}){1,2}$"


class FieldType(NamedTuple):
    name: str
    dbapi_type: list[int]
    pa_type: Callable[[ResultMetadataV2], DataType]


def vector_pa_type(metadata: ResultMetadataV2) -> DataType:
    """
    Generate the Arrow type represented by the given vector column metadata.
    Vectors are represented as Arrow fixed-size lists.
    """
    assert (
        metadata.fields is not None and len(metadata.fields) == 1
    ), "Invalid result metadata for vector type: expected a single field to be defined"
    assert (
        metadata.vector_dimension or 0
    ) > 0, "Invalid result metadata for vector type: expected a positive dimension"

    field_type = FIELD_TYPES[metadata.fields[0].type_code]
    return pa.list_(field_type.pa_type(metadata.fields[0]), metadata.vector_dimension)


def array_pa_type(metadata: ResultMetadataV2) -> DataType:
    """
    Generate the Arrow type represented by the given array column metadata.
    """
    # If fields is missing then structured types are not enabled.
    # Fallback to json encoded string
    if metadata.fields is None:
        return pa.string()

    assert (
        len(metadata.fields) == 1
    ), "Invalid result metadata for array type: expected a single field to be defined"

    field_type = FIELD_TYPES[metadata.fields[0].type_code]
    return pa.list_(field_type.pa_type(metadata.fields[0]))


def map_pa_type(metadata: ResultMetadataV2) -> DataType:
    """
    Generate the Arrow type represented by the given map column metadata.
    """
    # If fields is missing then structured types are not enabled.
    # Fallback to json encoded string
    if metadata.fields is None:
        return pa.string()

    assert (
        len(metadata.fields or []) == 2
    ), "Invalid result metadata for map type: expected a field for key and a field for value"
    key_type = FIELD_TYPES[metadata.fields[0].type_code]
    value_type = FIELD_TYPES[metadata.fields[1].type_code]
    return pa.map_(
        key_type.pa_type(metadata.fields[0]), value_type.pa_type(metadata.fields[1])
    )


def struct_pa_type(metadata: ResultMetadataV2) -> DataType:
    """
    Generate the Arrow type represented by the given struct column metadata.
    """
    # If fields is missing then structured types are not enabled.
    # Fallback to json encoded string
    if metadata.fields is None:
        return pa.string()

    assert all(
        field.name is not None for field in metadata.fields
    ), "All fields of a stuct type must have a name."
    return pa.struct(
        {
            field.name: FIELD_TYPES[field.type_code].pa_type(field)
            for field in metadata.fields
        }
    )


# This type mapping holds column type definitions.
#  Be careful to not change the ordering as the index is what Snowflake
#  gives to as schema
#
# `name` is the SQL name of the type, `dbapi_type` is the set of corresponding
# PEP 249 type objects, and `pa_type` is a lambda that takes in a column's
# result metadata and returns the corresponding Arrow type.
FIELD_TYPES: tuple[FieldType, ...] = (
    FieldType(
        name="FIXED", dbapi_type=[DBAPI_TYPE_NUMBER], pa_type=lambda _: pa.int64()
    ),
    FieldType(
        name="REAL", dbapi_type=[DBAPI_TYPE_NUMBER], pa_type=lambda _: pa.float64()
    ),
    FieldType(
        name="TEXT", dbapi_type=[DBAPI_TYPE_STRING], pa_type=lambda _: pa.string()
    ),
    FieldType(
        name="DATE", dbapi_type=[DBAPI_TYPE_TIMESTAMP], pa_type=lambda _: pa.date64()
    ),
    FieldType(
        name="TIMESTAMP",
        dbapi_type=[DBAPI_TYPE_TIMESTAMP],
        pa_type=lambda _: pa.time64("ns"),
    ),
    FieldType(
        name="VARIANT", dbapi_type=[DBAPI_TYPE_BINARY], pa_type=lambda _: pa.string()
    ),
    FieldType(
        name="TIMESTAMP_LTZ",
        dbapi_type=[DBAPI_TYPE_TIMESTAMP],
        pa_type=lambda _: pa.timestamp("ns"),
    ),
    FieldType(
        name="TIMESTAMP_TZ",
        dbapi_type=[DBAPI_TYPE_TIMESTAMP],
        pa_type=lambda _: pa.timestamp("ns"),
    ),
    FieldType(
        name="TIMESTAMP_NTZ",
        dbapi_type=[DBAPI_TYPE_TIMESTAMP],
        pa_type=lambda _: pa.timestamp("ns"),
    ),
    FieldType(name="OBJECT", dbapi_type=[DBAPI_TYPE_BINARY], pa_type=struct_pa_type),
    FieldType(name="ARRAY", dbapi_type=[DBAPI_TYPE_BINARY], pa_type=array_pa_type),
    FieldType(
        name="BINARY", dbapi_type=[DBAPI_TYPE_BINARY], pa_type=lambda _: pa.binary()
    ),
    FieldType(
        name="TIME",
        dbapi_type=[DBAPI_TYPE_TIMESTAMP],
        pa_type=lambda _: pa.time64("ns"),
    ),
    FieldType(name="BOOLEAN", dbapi_type=[], pa_type=lambda _: pa.bool_()),
    FieldType(
        name="GEOGRAPHY", dbapi_type=[DBAPI_TYPE_STRING], pa_type=lambda _: pa.string()
    ),
    FieldType(
        name="GEOMETRY", dbapi_type=[DBAPI_TYPE_STRING], pa_type=lambda _: pa.string()
    ),
    FieldType(name="VECTOR", dbapi_type=[DBAPI_TYPE_BINARY], pa_type=vector_pa_type),
    FieldType(name="MAP", dbapi_type=[DBAPI_TYPE_BINARY], pa_type=map_pa_type),
)

FIELD_NAME_TO_ID: DefaultDict[Any, int] = defaultdict(int)
FIELD_ID_TO_NAME: DefaultDict[int, str] = defaultdict(str)

__binary_types: list[int] = []
__binary_type_names: list[str] = []
__string_types: list[int] = []
__string_type_names: list[str] = []
__number_types: list[int] = []
__number_type_names: list[str] = []
__timestamp_types: list[int] = []
__timestamp_type_names: list[str] = []

for idx, field_type in enumerate(FIELD_TYPES):
    FIELD_ID_TO_NAME[idx] = field_type.name
    FIELD_NAME_TO_ID[field_type.name] = idx

    dbapi_types = field_type.dbapi_type
    for dbapi_type in dbapi_types:
        if dbapi_type == DBAPI_TYPE_BINARY:
            __binary_types.append(idx)
            __binary_type_names.append(field_type.name)
        elif dbapi_type == DBAPI_TYPE_TIMESTAMP:
            __timestamp_types.append(idx)
            __timestamp_type_names.append(field_type.name)
        elif dbapi_type == DBAPI_TYPE_NUMBER:
            __number_types.append(idx)
            __number_type_names.append(field_type.name)
        elif dbapi_type == DBAPI_TYPE_STRING:
            __string_types.append(idx)
            __string_type_names.append(field_type.name)


def get_binary_types() -> list[int]:
    return __binary_types


def is_binary_type_name(type_name: str) -> bool:
    return type_name in __binary_type_names


def get_string_types() -> list[int]:
    return __string_types


def is_string_type_name(type_name) -> bool:
    return type_name in __string_type_names


def get_number_types() -> list[int]:
    return __number_types


def is_number_type_name(type_name) -> bool:
    return type_name in __number_type_names


def get_timestamp_types() -> list[int]:
    return __timestamp_types


def is_timestamp_type_name(type_name) -> bool:
    return type_name in __timestamp_type_names


def is_date_type_name(type_name) -> bool:
    return type_name == "DATE"


# Log format
LOG_FORMAT = (
    "%(asctime)s - %(filename)s:%(lineno)d - "
    "%(funcName)s() - %(levelname)s - %(message)s"
)

# String literals
UTF8 = "utf-8"
SHA256_DIGEST = "sha256_digest"

# PUT/GET related
S3_FS = "S3"
AZURE_FS = "AZURE"
GCS_FS = "GCS"
LOCAL_FS = "LOCAL_FS"
CMD_TYPE_UPLOAD = "UPLOAD"
CMD_TYPE_DOWNLOAD = "DOWNLOAD"
FILE_PROTOCOL = "file://"


@unique
class ResultStatus(Enum):
    ERROR = "ERROR"
    SUCCEEDED = "SUCCEEDED"
    UPLOADED = "UPLOADED"
    DOWNLOADED = "DOWNLOADED"
    COLLISION = "COLLISION"
    SKIPPED = "SKIPPED"
    RENEW_TOKEN = "RENEW_TOKEN"
    RENEW_PRESIGNED_URL = "RENEW_PRESIGNED_URL"
    NOT_FOUND_FILE = "NOT_FOUND_FILE"
    NEED_RETRY = "NEED_RETRY"
    NEED_RETRY_WITH_LOWER_CONCURRENCY = "NEED_RETRY_WITH_LOWER_CONCURRENCY"


class SnowflakeS3FileEncryptionMaterial(NamedTuple):
    query_id: str
    query_stage_master_key: str
    smk_id: int


class MaterialDescriptor(NamedTuple):
    smk_id: int
    query_id: str
    key_size: int


class EncryptionMetadata(NamedTuple):
    key: str
    iv: str
    matdesc: str


class FileHeader(NamedTuple):
    digest: str | None
    content_length: int | None
    encryption_metadata: EncryptionMetadata | None


PARAMETER_AUTOCOMMIT = "AUTOCOMMIT"
PARAMETER_CLIENT_SESSION_KEEP_ALIVE_HEARTBEAT_FREQUENCY = (
    "CLIENT_SESSION_KEEP_ALIVE_HEARTBEAT_FREQUENCY"
)
PARAMETER_CLIENT_SESSION_KEEP_ALIVE = "CLIENT_SESSION_KEEP_ALIVE"
PARAMETER_CLIENT_PREFETCH_THREADS = "CLIENT_PREFETCH_THREADS"
PARAMETER_CLIENT_TELEMETRY_ENABLED = "CLIENT_TELEMETRY_ENABLED"
PARAMETER_CLIENT_TELEMETRY_OOB_ENABLED = "CLIENT_OUT_OF_BAND_TELEMETRY_ENABLED"
PARAMETER_CLIENT_STORE_TEMPORARY_CREDENTIAL = "CLIENT_STORE_TEMPORARY_CREDENTIAL"
PARAMETER_CLIENT_REQUEST_MFA_TOKEN = "CLIENT_REQUEST_MFA_TOKEN"
PARAMETER_CLIENT_USE_SECURE_STORAGE_FOR_TEMPORARY_CREDENTIAL = (
    "CLIENT_USE_SECURE_STORAGE_FOR_TEMPORARY_CREDENTAIL"
)
PARAMETER_QUERY_CONTEXT_CACHE_SIZE = "QUERY_CONTEXT_CACHE_SIZE"
PARAMETER_TIMEZONE = "TIMEZONE"
PARAMETER_SERVICE_NAME = "SERVICE_NAME"
PARAMETER_CLIENT_VALIDATE_DEFAULT_PARAMETERS = "CLIENT_VALIDATE_DEFAULT_PARAMETERS"
PARAMETER_PYTHON_CONNECTOR_QUERY_RESULT_FORMAT = "PYTHON_CONNECTOR_QUERY_RESULT_FORMAT"
PARAMETER_ENABLE_STAGE_S3_PRIVATELINK_FOR_US_EAST_1 = (
    "ENABLE_STAGE_S3_PRIVATELINK_FOR_US_EAST_1"
)
PARAMETER_MULTI_STATEMENT_COUNT = "MULTI_STATEMENT_COUNT"

HTTP_HEADER_CONTENT_TYPE = "Content-Type"
HTTP_HEADER_CONTENT_ENCODING = "Content-Encoding"
HTTP_HEADER_ACCEPT_ENCODING = "Accept-Encoding"
HTTP_HEADER_ACCEPT = "accept"
HTTP_HEADER_USER_AGENT = "User-Agent"
HTTP_HEADER_SERVICE_NAME = "X-Snowflake-Service"

HTTP_HEADER_VALUE_OCTET_STREAM = "application/octet-stream"


@unique
class OCSPMode(Enum):
    """OCSP Mode enumerator for all the available modes.

    OCSP mode descriptions:
        FAIL_CLOSED: If the client or driver does not receive a valid OCSP CA response for any reason,
            the connection fails.
        FAIL_OPEN: A response indicating a revoked certificate results in a failed connection. A response with any
            other certificate errors or statuses allows the connection to occur, but denotes the message in the logs
            at the WARNING level with the relevant details in JSON format.
        INSECURE: The connection will occur anyway.
    """

    FAIL_CLOSED = "FAIL_CLOSED"
    FAIL_OPEN = "FAIL_OPEN"
    INSECURE = "INSECURE"


@unique
class FileTransferType(Enum):
    """This enum keeps track of the possible file transfer types."""

    PUT = auto()
    GET = auto()


@unique
class QueryStatus(Enum):
    RUNNING = 0
    ABORTING = 1
    SUCCESS = 2
    FAILED_WITH_ERROR = 3
    ABORTED = 4
    QUEUED = 5
    FAILED_WITH_INCIDENT = 6
    DISCONNECTED = 7
    RESUMING_WAREHOUSE = 8
    # purposeful typo. Is present in QueryDTO.java
    QUEUED_REPARING_WAREHOUSE = 9
    RESTARTED = 10
    BLOCKED = 11
    NO_DATA = 12


# Size constants
kilobyte = 1024
megabyte = kilobyte * 1024
gigabyte = megabyte * 1024


# ArrowResultChunk constants the unit in this iterator
# EMPTY_UNIT: default
# ROW_UNIT: fetch row by row if the user call `fetchone()`
# TABLE_UNIT: fetch one arrow table if the user call `fetch_pandas()`
@unique
class IterUnit(Enum):
    ROW_UNIT = "row"
    TABLE_UNIT = "table"


# File Transfer
# Amazon S3 multipart upload limits
# https://docs.aws.amazon.com/AmazonS3/latest/userguide/qfacts.html
S3_DEFAULT_CHUNK_SIZE = 8 * 1024**2
S3_MAX_OBJECT_SIZE = 5 * 1024**4
S3_MAX_PART_SIZE = 5 * 1024**3
S3_MIN_PART_SIZE = 5 * 1024**2
S3_MAX_PARTS = 10000

S3_CHUNK_SIZE = 8388608  # boto3 default
AZURE_CHUNK_SIZE = 4 * megabyte

# https://requests.readthedocs.io/en/latest/user/advanced/#timeouts
REQUEST_CONNECTION_TIMEOUT = 10
REQUEST_READ_TIMEOUT = 600

DAY_IN_SECONDS = 60 * 60 * 24

# TODO: all env variables definitions should be here
ENV_VAR_PARTNER = "SF_PARTNER"
ENV_VAR_TEST_MODE = "SNOWFLAKE_TEST_MODE"


_DOMAIN_NAME_MAP = {_DEFAULT_HOSTNAME_TLD: "GLOBAL", _CHINA_HOSTNAME_TLD: "CHINA"}

_CONNECTIVITY_ERR_MSG = (
    "Verify that the hostnames and port numbers in SYSTEM$ALLOWLIST are added to your firewall's allowed list."
    "\nTo further troubleshoot your connection you may reference the following article: "
    "https://docs.snowflake.com/en/user-guide/client-connectivity-troubleshooting/overview."
)

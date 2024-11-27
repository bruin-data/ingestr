# -*- coding: utf-8 -*-
from __future__ import annotations

import logging
import re
from datetime import datetime
from typing import Any, Dict, List, Optional, Pattern

from pyathena.error import DataError

_logger = logging.getLogger(__name__)  # type: ignore


class AthenaQueryExecution:
    STATE_QUEUED: str = "QUEUED"
    STATE_RUNNING: str = "RUNNING"
    STATE_SUCCEEDED: str = "SUCCEEDED"
    STATE_FAILED: str = "FAILED"
    STATE_CANCELLED: str = "CANCELLED"

    STATEMENT_TYPE_DDL: str = "DDL"
    STATEMENT_TYPE_DML: str = "DML"
    STATEMENT_TYPE_UTILITY: str = "UTILITY"

    ENCRYPTION_OPTION_SSE_S3: str = "SSE_S3"
    ENCRYPTION_OPTION_SSE_KMS: str = "SSE_KMS"
    ENCRYPTION_OPTION_CSE_KMS: str = "CSE_KMS"

    ERROR_CATEGORY_SYSTEM: int = 1
    ERROR_CATEGORY_USER: int = 2
    ERROR_CATEGORY_OTHER: int = 3

    S3_ACL_OPTION_BUCKET_OWNER_FULL_CONTROL = "BUCKET_OWNER_FULL_CONTROL"

    def __init__(self, response: Dict[str, Any]) -> None:
        query_execution = response.get("QueryExecution")
        if not query_execution:
            raise DataError("KeyError `QueryExecution`")

        query_execution_context = query_execution.get("QueryExecutionContext", {})
        self._database: Optional[str] = query_execution_context.get("Database")
        self._catalog: Optional[str] = query_execution_context.get("Catalog")

        self._query_id: Optional[str] = query_execution.get("QueryExecutionId")
        if not self._query_id:
            raise DataError("KeyError `QueryExecutionId`")
        self._query: Optional[str] = query_execution.get("Query")
        if not self._query:
            raise DataError("KeyError `Query`")
        self._statement_type: Optional[str] = query_execution.get("StatementType")
        self._substatement_type: Optional[str] = query_execution.get("SubstatementType")
        self._work_group: Optional[str] = query_execution.get("WorkGroup")
        self._execution_parameters: List[str] = query_execution.get("ExecutionParameters", [])

        status = query_execution.get("Status")
        if not status:
            raise DataError("KeyError `Status`")
        self._state: Optional[str] = status.get("State")
        self._state_change_reason: Optional[str] = status.get("StateChangeReason")
        self._submission_date_time: Optional[datetime] = status.get("SubmissionDateTime")
        self._completion_date_time: Optional[datetime] = status.get("CompletionDateTime")
        athena_error = status.get("AthenaError", {})
        self._error_category: Optional[int] = athena_error.get("ErrorCategory")
        self._error_type: Optional[int] = athena_error.get("ErrorType")
        self._retryable: Optional[bool] = athena_error.get("Retryable")
        self._error_message: Optional[str] = athena_error.get("ErrorMessage")

        statistics = query_execution.get("Statistics", {})
        self._data_scanned_in_bytes: Optional[int] = statistics.get("DataScannedInBytes")
        self._engine_execution_time_in_millis: Optional[int] = statistics.get(
            "EngineExecutionTimeInMillis", None
        )
        self._query_queue_time_in_millis: Optional[int] = statistics.get(
            "QueryQueueTimeInMillis", None
        )
        self._total_execution_time_in_millis: Optional[int] = statistics.get(
            "TotalExecutionTimeInMillis", None
        )
        self._query_planning_time_in_millis: Optional[int] = statistics.get(
            "QueryPlanningTimeInMillis", None
        )
        self._service_processing_time_in_millis: Optional[int] = statistics.get(
            "ServiceProcessingTimeInMillis", None
        )
        self._data_manifest_location: Optional[str] = statistics.get("DataManifestLocation")
        reuse_info = statistics.get("ResultReuseInformation", {})
        self._reused_previous_result: Optional[bool] = reuse_info.get("ReusedPreviousResult")

        result_conf = query_execution.get("ResultConfiguration", {})
        self._output_location: Optional[str] = result_conf.get("OutputLocation")
        encryption_conf = result_conf.get("EncryptionConfiguration", {})
        self._encryption_option: Optional[str] = encryption_conf.get("EncryptionOption")
        self._kms_key: Optional[str] = encryption_conf.get("KmsKey")
        self._expected_bucket_owner: Optional[str] = result_conf.get("ExpectedBucketOwner")
        acl_conf = result_conf.get("AclConfiguration", {})
        self._s3_acl_option: Optional[str] = acl_conf.get("S3AclOption")

        engine_version = query_execution.get("EngineVersion", {})
        self._selected_engine_version: Optional[str] = engine_version.get(
            "SelectedEngineVersion", None
        )
        self._effective_engine_version: Optional[str] = engine_version.get(
            "EffectiveEngineVersion", None
        )

        reuse_conf = query_execution.get("ResultReuseConfiguration", {})
        reuse_age_conf = reuse_conf.get("ResultReuseByAgeConfiguration", {})
        self._result_reuse_enabled: Optional[bool] = reuse_age_conf.get("Enabled")
        self._result_reuse_minutes: Optional[int] = reuse_age_conf.get("MaxAgeInMinutes")

    @property
    def database(self) -> Optional[str]:
        return self._database

    @property
    def catalog(self) -> Optional[str]:
        return self._catalog

    @property
    def query_id(self) -> Optional[str]:
        return self._query_id

    @property
    def query(self) -> Optional[str]:
        return self._query

    @property
    def statement_type(self) -> Optional[str]:
        return self._statement_type

    @property
    def substatement_type(self) -> Optional[str]:
        return self._substatement_type

    @property
    def work_group(self) -> Optional[str]:
        return self._work_group

    @property
    def execution_parameters(self) -> List[str]:
        return self._execution_parameters

    @property
    def state(self) -> Optional[str]:
        return self._state

    @property
    def state_change_reason(self) -> Optional[str]:
        return self._state_change_reason

    @property
    def submission_date_time(self) -> Optional[datetime]:
        return self._submission_date_time

    @property
    def completion_date_time(self) -> Optional[datetime]:
        return self._completion_date_time

    @property
    def error_category(self) -> Optional[int]:
        return self._error_category

    @property
    def error_type(self) -> Optional[int]:
        return self._error_type

    @property
    def retryable(self) -> Optional[bool]:
        return self._retryable

    @property
    def error_message(self) -> Optional[str]:
        return self._error_message

    @property
    def data_scanned_in_bytes(self) -> Optional[int]:
        return self._data_scanned_in_bytes

    @property
    def engine_execution_time_in_millis(self) -> Optional[int]:
        return self._engine_execution_time_in_millis

    @property
    def query_queue_time_in_millis(self) -> Optional[int]:
        return self._query_queue_time_in_millis

    @property
    def total_execution_time_in_millis(self) -> Optional[int]:
        return self._total_execution_time_in_millis

    @property
    def query_planning_time_in_millis(self) -> Optional[int]:
        return self._query_planning_time_in_millis

    @property
    def service_processing_time_in_millis(self) -> Optional[int]:
        return self._service_processing_time_in_millis

    @property
    def output_location(self) -> Optional[str]:
        return self._output_location

    @property
    def data_manifest_location(self) -> Optional[str]:
        return self._data_manifest_location

    @property
    def reused_previous_result(self) -> Optional[bool]:
        return self._reused_previous_result

    @property
    def encryption_option(self) -> Optional[str]:
        return self._encryption_option

    @property
    def kms_key(self) -> Optional[str]:
        return self._kms_key

    @property
    def expected_bucket_owner(self) -> Optional[str]:
        return self._expected_bucket_owner

    @property
    def s3_acl_option(self) -> Optional[str]:
        return self._s3_acl_option

    @property
    def selected_engine_version(self) -> Optional[str]:
        return self._selected_engine_version

    @property
    def effective_engine_version(self) -> Optional[str]:
        return self._effective_engine_version

    @property
    def result_reuse_enabled(self) -> Optional[bool]:
        return self._result_reuse_enabled

    @property
    def result_reuse_minutes(self) -> Optional[int]:
        return self._result_reuse_minutes


class AthenaCalculationExecutionStatus:
    STATE_CREATING: str = "CREATING"
    STATE_CREATED: str = "CREATED"
    STATE_QUEUED: str = "QUEUED"
    STATE_RUNNING: str = "RUNNING"
    STATE_CANCELING: str = "CANCELING"
    STATE_CANCELED: str = "CANCELED"
    STATE_COMPLETED: str = "COMPLETED"
    STATE_FAILED: str = "FAILED"

    def __init__(self, response: Dict[str, Any]) -> None:
        status = response.get("Status")
        if not status:
            raise DataError("KeyError `Status`")
        self._state: Optional[str] = status.get("State")
        self._state_change_reason: Optional[str] = status.get("StateChangeReason")
        self._submission_date_time: Optional[datetime] = status.get("SubmissionDateTime")
        self._completion_date_time: Optional[datetime] = status.get("CompletionDateTime")

        statistics = response.get("Statistics")
        if not statistics:
            raise DataError("KeyError `Statistics`")
        self._dpu_execution_in_millis: Optional[int] = statistics.get("DpuExecutionInMillis")
        self._progress: Optional[str] = statistics.get("Progress")

    @property
    def state(self) -> Optional[str]:
        return self._state

    @property
    def state_change_reason(self) -> Optional[str]:
        return self._state_change_reason

    @property
    def submission_date_time(self) -> Optional[datetime]:
        return self._submission_date_time

    @property
    def completion_date_time(self) -> Optional[datetime]:
        return self._completion_date_time

    @property
    def dpu_execution_in_millis(self) -> Optional[int]:
        return self._dpu_execution_in_millis

    @property
    def progress(self) -> Optional[str]:
        return self._progress


class AthenaCalculationExecution(AthenaCalculationExecutionStatus):
    def __init__(self, response: Dict[str, Any]) -> None:
        super().__init__(response)

        self._calculation_id: Optional[str] = response.get("CalculationExecutionId")
        if not self._calculation_id:
            raise DataError("KeyError `CalculationExecutionId`")
        self._session_id: Optional[str] = response.get("SessionId")
        if not self._session_id:
            raise DataError("KeyError `SessionId`")
        self._description: Optional[str] = response.get("Description")
        self._working_directory: Optional[str] = response.get("WorkingDirectory")

        # If cancelled, the result does not exist.
        result = response.get("Result", {})
        self._std_out_s3_uri: Optional[str] = result.get("StdOutS3Uri")
        self._std_error_s3_uri: Optional[str] = result.get("StdErrorS3Uri")
        self._result_s3_uri: Optional[str] = result.get("ResultS3Uri")
        self._result_type: Optional[str] = result.get("ResultType")

    @property
    def calculation_id(self) -> Optional[str]:
        return self._calculation_id

    @property
    def session_id(self) -> Optional[str]:
        return self._session_id

    @property
    def description(self) -> Optional[str]:
        return self._description

    @property
    def working_directory(self) -> Optional[str]:
        return self._working_directory

    @property
    def std_out_s3_uri(self) -> Optional[str]:
        return self._std_out_s3_uri

    @property
    def std_error_s3_uri(self) -> Optional[str]:
        return self._std_error_s3_uri

    @property
    def result_s3_uri(self) -> Optional[str]:
        return self._result_s3_uri

    @property
    def result_type(self) -> Optional[str]:
        return self._result_type


class AthenaSessionStatus:
    STATE_CREATING: str = "CREATING"
    STATE_CREATED: str = "CREATED"
    STATE_IDLE: str = "IDLE"
    STATE_BUSY: str = "BUSY"
    STATE_TERMINATING: str = "TERMINATING"
    STATE_TERMINATED: str = "TERMINATED"
    STATE_DEGRADED: str = "DEGRADED"
    STATE_FAILED: str = "FAILED"

    def __init__(self, response: Dict[str, Any]) -> None:
        self._session_id: Optional[str] = response.get("SessionId")

        status = response.get("Status")
        if not status:
            raise DataError("KeyError `Status`")
        self._state: Optional[str] = status.get("State")
        self._state_change_reason: Optional[str] = status.get("StateChangeReason")
        self._start_date_time: Optional[datetime] = status.get("StartDateTime")
        self._last_modified_date_time: Optional[datetime] = status.get("LastModifiedDateTime")
        self._end_date_time: Optional[datetime] = status.get("EndDateTime")
        self._idle_since_date_time: Optional[datetime] = status.get("IdleSinceDateTime")

    @property
    def session_id(self) -> Optional[str]:
        return self._session_id

    @property
    def state(self) -> Optional[str]:
        return self._state

    @property
    def state_change_reason(self) -> Optional[str]:
        return self._state_change_reason

    @property
    def start_date_time(self) -> Optional[datetime]:
        return self._start_date_time

    @property
    def last_modified_date_time(self) -> Optional[datetime]:
        return self._last_modified_date_time

    @property
    def end_date_time(self) -> Optional[datetime]:
        return self._end_date_time

    @property
    def idle_since_date_time(self) -> Optional[datetime]:
        return self._idle_since_date_time


class AthenaDatabase:
    def __init__(self, response):
        database = response.get("Database")
        if not database:
            raise DataError("KeyError `Database`")

        self._name: Optional[str] = database.get("Name")
        self._description: Optional[str] = database.get("Description")
        self._parameters: Dict[str, str] = database.get("Parameters", {})

    @property
    def name(self) -> Optional[str]:
        return self._name

    @property
    def description(self) -> Optional[str]:
        return self._description

    @property
    def parameters(self) -> Dict[str, str]:
        return self._parameters


class AthenaTableMetadataColumn:
    def __init__(self, response):
        self._name: Optional[str] = response.get("Name")
        self._type: Optional[str] = response.get("Type")
        self._comment: Optional[str] = response.get("Comment")

    @property
    def name(self) -> Optional[str]:
        return self._name

    @property
    def type(self) -> Optional[str]:
        return self._type

    @property
    def comment(self) -> Optional[str]:
        return self._comment


class AthenaTableMetadataPartitionKey:
    def __init__(self, response):
        self._name: Optional[str] = response.get("Name")
        self._type: Optional[str] = response.get("Type")
        self._comment: Optional[str] = response.get("Comment")

    @property
    def name(self) -> Optional[str]:
        return self._name

    @property
    def type(self) -> Optional[str]:
        return self._type

    @property
    def comment(self) -> Optional[str]:
        return self._comment


class AthenaTableMetadata:
    def __init__(self, response):
        table_metadata = response.get("TableMetadata")
        if not table_metadata:
            raise DataError("KeyError `TableMetadata`")

        self._name: Optional[str] = table_metadata.get("Name")
        self._create_time: Optional[datetime] = table_metadata.get("CreateTime")
        self._last_access_time: Optional[datetime] = table_metadata.get("LastAccessTime")
        self._table_type: Optional[str] = table_metadata.get("TableType")

        columns = table_metadata.get("Columns", [])
        self._columns: List[AthenaTableMetadataColumn] = []
        for column in columns:
            self._columns.append(AthenaTableMetadataColumn(column))

        partition_keys = table_metadata.get("PartitionKeys", [])
        self._partition_keys: List[AthenaTableMetadataPartitionKey] = []
        for key in partition_keys:
            self._partition_keys.append(AthenaTableMetadataPartitionKey(key))

        self._parameters: Dict[str, str] = table_metadata.get("Parameters", {})

    @property
    def name(self) -> Optional[str]:
        return self._name

    @property
    def create_time(self) -> Optional[datetime]:
        return self._create_time

    @property
    def last_access_time(self) -> Optional[datetime]:
        return self._last_access_time

    @property
    def table_type(self) -> Optional[str]:
        return self._table_type

    @property
    def columns(self) -> List[AthenaTableMetadataColumn]:
        return self._columns

    @property
    def partition_keys(self) -> List[AthenaTableMetadataPartitionKey]:
        return self._partition_keys

    @property
    def parameters(self) -> Dict[str, str]:
        return self._parameters

    @property
    def comment(self) -> Optional[str]:
        return self._parameters.get("comment")

    @property
    def location(self) -> Optional[str]:
        return self._parameters.get("location")

    @property
    def input_format(self) -> Optional[str]:
        return self._parameters.get("inputformat")

    @property
    def output_format(self) -> Optional[str]:
        return self._parameters.get("outputformat")

    @property
    def row_format(self) -> Optional[str]:
        serde = self.serde_serialization_lib
        if serde:
            return f"SERDE '{serde}'"
        return None

    @property
    def file_format(self) -> Optional[str]:
        input = self.input_format
        output = self.output_format
        if input and output:
            return f"INPUTFORMAT '{input}' OUTPUTFORMAT '{output}'"
        return None

    @property
    def serde_serialization_lib(self) -> Optional[str]:
        return self._parameters.get("serde.serialization.lib")

    @property
    def compression(self) -> Optional[str]:
        if "write.compression" in self._parameters:  # text or json
            return self._parameters["write.compression"]
        elif "serde.param.write.compression" in self._parameters:  # text or json
            return self._parameters["serde.param.write.compression"]
        elif "parquet.compress" in self._parameters:  # parquet
            return self._parameters["parquet.compress"]
        elif "orc.compress" in self._parameters:  # orc
            return self._parameters["orc.compress"]
        else:
            return None

    @property
    def serde_properties(self) -> Dict[str, str]:
        return {
            k.replace("serde.param.", ""): v
            for k, v in self._parameters.items()
            if k.startswith("serde.param.")
        }

    @property
    def table_properties(self) -> Dict[str, str]:
        return {k: v for k, v in self._parameters.items() if not k.startswith("serde.param.")}


class AthenaFileFormat:
    FILE_FORMAT_SEQUENCEFILE: str = "SEQUENCEFILE"
    FILE_FORMAT_TEXTFILE: str = "TEXTFILE"
    FILE_FORMAT_RCFILE: str = "RCFILE"
    FILE_FORMAT_ORC: str = "ORC"
    FILE_FORMAT_PARQUET: str = "PARQUET"
    FILE_FORMAT_AVRO: str = "AVRO"
    FILE_FORMAT_ION: str = "ION"

    @staticmethod
    def is_parquet(value: str) -> bool:
        return value.upper() == AthenaFileFormat.FILE_FORMAT_PARQUET

    @staticmethod
    def is_orc(value: str) -> bool:
        return value.upper() == AthenaFileFormat.FILE_FORMAT_ORC


class AthenaRowFormatSerde:
    PATTERN_ROW_FORMAT_SERDE: Pattern[str] = re.compile(r"^(?i:serde) '(?P<serde>.+)'$")

    ROW_FORMAT_SERDE_CSV: str = "org.apache.hadoop.hive.serde2.OpenCSVSerde"
    ROW_FORMAT_SERDE_REGEX: str = "org.apache.hadoop.hive.serde2.RegexSerDe"
    ROW_FORMAT_SERDE_LAZY_SIMPLE: str = "org.apache.hadoop.hive.serde2.lazy.LazySimpleSerDe"
    ROW_FORMAT_SERDE_CLOUD_TRAIL: str = "com.amazon.emr.hive.serde.CloudTrailSerde"
    ROW_FORMAT_SERDE_GROK: str = "com.amazonaws.glue.serde.GrokSerDe"
    ROW_FORMAT_SERDE_JSON: str = "org.openx.data.jsonserde.JsonSerDe"
    ROW_FORMAT_SERDE_JSON_HCATALOG: str = "org.apache.hive.hcatalog.data.JsonSerDe"
    ROW_FORMAT_SERDE_PARQUET: str = "org.apache.hadoop.hive.ql.io.parquet.serde.ParquetHiveSerDe"
    ROW_FORMAT_SERDE_ORC: str = "org.apache.hadoop.hive.ql.io.orc.OrcSerde"
    ROW_FORMAT_SERDE_AVRO: str = "org.apache.hadoop.hive.serde2.avro.AvroSerDe"

    @staticmethod
    def is_parquet(value: str) -> bool:
        match = AthenaRowFormatSerde.PATTERN_ROW_FORMAT_SERDE.search(value)
        if match:
            serde = match.group("serde")
            if serde == AthenaRowFormatSerde.ROW_FORMAT_SERDE_PARQUET:
                return True
        return False

    @staticmethod
    def is_orc(value: str) -> bool:
        match = AthenaRowFormatSerde.PATTERN_ROW_FORMAT_SERDE.search(value)
        if match:
            serde = match.group("serde")
            if serde == AthenaRowFormatSerde.ROW_FORMAT_SERDE_ORC:
                return True
        return False


class AthenaCompression:
    COMPRESSION_BZIP2: str = "BZIP2"
    COMPRESSION_DEFLATE: str = "DEFLATE"
    COMPRESSION_GZIP: str = "GZIP"
    COMPRESSION_LZ4: str = "LZ4"
    COMPRESSION_LZO: str = "LZO"
    COMPRESSION_SNAPPY: str = "SNAPPY"
    COMPRESSION_ZLIB: str = "ZLIB"
    COMPRESSION_ZSTD: str = "ZSTD"

    @staticmethod
    def is_valid(value: str) -> bool:
        return value.upper() in [
            AthenaCompression.COMPRESSION_BZIP2,
            AthenaCompression.COMPRESSION_DEFLATE,
            AthenaCompression.COMPRESSION_GZIP,
            AthenaCompression.COMPRESSION_LZ4,
            AthenaCompression.COMPRESSION_LZO,
            AthenaCompression.COMPRESSION_SNAPPY,
            AthenaCompression.COMPRESSION_ZLIB,
            AthenaCompression.COMPRESSION_ZSTD,
        ]


class AthenaPartitionTransform:
    PARTITION_TRANSFORM_YEAR: str = "year"
    PARTITION_TRANSFORM_MONTH: str = "month"
    PARTITION_TRANSFORM_DAY: str = "day"
    PARTITION_TRANSFORM_HOUR: str = "hour"
    PARTITION_TRANSFORM_BUCKET: str = "bucket"
    PARTITION_TRANSFORM_TRUNCATE: str = "truncate"

    @staticmethod
    def is_valid(value: str) -> bool:
        return value.lower() in [
            AthenaPartitionTransform.PARTITION_TRANSFORM_YEAR,
            AthenaPartitionTransform.PARTITION_TRANSFORM_MONTH,
            AthenaPartitionTransform.PARTITION_TRANSFORM_DAY,
            AthenaPartitionTransform.PARTITION_TRANSFORM_HOUR,
            AthenaPartitionTransform.PARTITION_TRANSFORM_BUCKET,
            AthenaPartitionTransform.PARTITION_TRANSFORM_TRUNCATE,
        ]

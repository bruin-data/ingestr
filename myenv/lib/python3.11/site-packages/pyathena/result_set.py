# -*- coding: utf-8 -*-
from __future__ import annotations

import collections
import logging
from abc import abstractmethod
from datetime import datetime
from typing import (
    TYPE_CHECKING,
    Any,
    Deque,
    Dict,
    List,
    Optional,
    Tuple,
    Type,
    Union,
    cast,
)

from pyathena.common import CursorIterator
from pyathena.converter import Converter
from pyathena.error import DataError, OperationalError, ProgrammingError
from pyathena.model import AthenaQueryExecution
from pyathena.util import RetryConfig, parse_output_location, retry_api_call

if TYPE_CHECKING:
    from pyathena.connection import Connection

_logger = logging.getLogger(__name__)  # type: ignore


class AthenaResultSet(CursorIterator):
    def __init__(
        self,
        connection: "Connection[Any]",
        converter: Converter,
        query_execution: AthenaQueryExecution,
        arraysize: int,
        retry_config: RetryConfig,
    ) -> None:
        super().__init__(arraysize=arraysize)
        self._connection: Optional["Connection[Any]"] = connection
        self._converter = converter
        self._query_execution: Optional[AthenaQueryExecution] = query_execution
        assert self._query_execution, "Required argument `query_execution` not found."
        self._retry_config = retry_config
        self._client = connection.session.client(
            "s3",
            region_name=connection.region_name,
            config=connection.config,
            **connection._client_kwargs,
        )

        self._metadata: Optional[Tuple[Dict[str, Any], ...]] = None
        self._rows: Deque[
            Union[Tuple[Optional[Any], ...], Dict[Any, Optional[Any]]]
        ] = collections.deque()
        self._next_token: Optional[str] = None

        if self.state == AthenaQueryExecution.STATE_SUCCEEDED:
            self._rownumber = 0
            self._pre_fetch()

    @property
    def database(self) -> Optional[str]:
        if not self._query_execution:
            return None
        return self._query_execution.database

    @property
    def catalog(self) -> Optional[str]:
        if not self._query_execution:
            return None
        return self._query_execution.catalog

    @property
    def query_id(self) -> Optional[str]:
        if not self._query_execution:
            return None
        return self._query_execution.query_id

    @property
    def query(self) -> Optional[str]:
        if not self._query_execution:
            return None
        return self._query_execution.query

    @property
    def statement_type(self) -> Optional[str]:
        if not self._query_execution:
            return None
        return self._query_execution.statement_type

    @property
    def substatement_type(self) -> Optional[str]:
        if not self._query_execution:
            return None
        return self._query_execution.substatement_type

    @property
    def work_group(self) -> Optional[str]:
        if not self._query_execution:
            return None
        return self._query_execution.work_group

    @property
    def execution_parameters(self) -> List[str]:
        if not self._query_execution:
            return []
        return self._query_execution.execution_parameters

    @property
    def state(self) -> Optional[str]:
        if not self._query_execution:
            return None
        return self._query_execution.state

    @property
    def state_change_reason(self) -> Optional[str]:
        if not self._query_execution:
            return None
        return self._query_execution.state_change_reason

    @property
    def submission_date_time(self) -> Optional[datetime]:
        if not self._query_execution:
            return None
        return self._query_execution.submission_date_time

    @property
    def completion_date_time(self) -> Optional[datetime]:
        if not self._query_execution:
            return None
        return self._query_execution.completion_date_time

    @property
    def error_category(self) -> Optional[int]:
        if not self._query_execution:
            return None
        return self._query_execution.error_category

    @property
    def error_type(self) -> Optional[int]:
        if not self._query_execution:
            return None
        return self._query_execution.error_type

    @property
    def retryable(self) -> Optional[bool]:
        if not self._query_execution:
            return None
        return self._query_execution.retryable

    @property
    def error_message(self) -> Optional[str]:
        if not self._query_execution:
            return None
        return self._query_execution.error_message

    @property
    def data_scanned_in_bytes(self) -> Optional[int]:
        if not self._query_execution:
            return None
        return self._query_execution.data_scanned_in_bytes

    @property
    def engine_execution_time_in_millis(self) -> Optional[int]:
        if not self._query_execution:
            return None
        return self._query_execution.engine_execution_time_in_millis

    @property
    def query_queue_time_in_millis(self) -> Optional[int]:
        if not self._query_execution:
            return None
        return self._query_execution.query_queue_time_in_millis

    @property
    def total_execution_time_in_millis(self) -> Optional[int]:
        if not self._query_execution:
            return None
        return self._query_execution.total_execution_time_in_millis

    @property
    def query_planning_time_in_millis(self) -> Optional[int]:
        if not self._query_execution:
            return None
        return self._query_execution.query_planning_time_in_millis

    @property
    def service_processing_time_in_millis(self) -> Optional[int]:
        if not self._query_execution:
            return None
        return self._query_execution.service_processing_time_in_millis

    @property
    def output_location(self) -> Optional[str]:
        if not self._query_execution:
            return None
        return self._query_execution.output_location

    @property
    def data_manifest_location(self) -> Optional[str]:
        if not self._query_execution:
            return None
        return self._query_execution.data_manifest_location

    @property
    def reused_previous_result(self) -> Optional[bool]:
        if not self._query_execution:
            return None
        return self._query_execution.reused_previous_result

    @property
    def encryption_option(self) -> Optional[str]:
        if not self._query_execution:
            return None
        return self._query_execution.encryption_option

    @property
    def kms_key(self) -> Optional[str]:
        if not self._query_execution:
            return None
        return self._query_execution.kms_key

    @property
    def expected_bucket_owner(self) -> Optional[str]:
        if not self._query_execution:
            return None
        return self._query_execution.expected_bucket_owner

    @property
    def s3_acl_option(self) -> Optional[str]:
        if not self._query_execution:
            return None
        return self._query_execution.s3_acl_option

    @property
    def selected_engine_version(self) -> Optional[str]:
        if not self._query_execution:
            return None
        return self._query_execution.selected_engine_version

    @property
    def effective_engine_version(self) -> Optional[str]:
        if not self._query_execution:
            return None
        return self._query_execution.effective_engine_version

    @property
    def result_reuse_enabled(self) -> Optional[bool]:
        if not self._query_execution:
            return None
        return self._query_execution.result_reuse_enabled

    @property
    def result_reuse_minutes(self) -> Optional[int]:
        if not self._query_execution:
            return None
        return self._query_execution.result_reuse_minutes

    @property
    def description(
        self,
    ) -> Optional[List[Tuple[str, str, None, None, int, int, str]]]:
        if self._metadata is None:
            return None
        return [
            (
                m["Name"],
                m["Type"],
                None,
                None,
                m["Precision"],
                m["Scale"],
                m["Nullable"],
            )
            for m in self._metadata
        ]

    @property
    def connection(self) -> "Connection[Any]":
        if self.is_closed:
            raise ProgrammingError("AthenaResultSet is closed.")
        return cast("Connection[Any]", self._connection)

    def __fetch(self, next_token: Optional[str] = None) -> Dict[str, Any]:
        if not self.query_id:
            raise ProgrammingError("QueryExecutionId is none or empty.")
        if self.state != AthenaQueryExecution.STATE_SUCCEEDED:
            raise ProgrammingError("QueryExecutionState is not SUCCEEDED.")
        if self.is_closed:
            raise ProgrammingError("AthenaResultSet is closed.")
        request = {
            "QueryExecutionId": self.query_id,
            "MaxResults": self._arraysize,
        }
        if next_token:
            request.update({"NextToken": next_token})
        try:
            response = retry_api_call(
                self.connection.client.get_query_results,
                config=self._retry_config,
                logger=_logger,
                **request,
            )
        except Exception as e:
            _logger.exception("Failed to fetch result set.")
            raise OperationalError(*e.args) from e
        else:
            return cast(Dict[str, Any], response)

    def _fetch(self) -> None:
        if not self._next_token:
            raise ProgrammingError("NextToken is none or empty.")
        response = self.__fetch(self._next_token)
        self._process_rows(response)

    def _pre_fetch(self) -> None:
        response = self.__fetch()
        self._process_metadata(response)
        self._process_update_count(response)
        self._process_rows(response)

    def fetchone(
        self,
    ) -> Optional[Union[Tuple[Optional[Any], ...], Dict[Any, Optional[Any]]]]:
        if not self._rows and self._next_token:
            self._fetch()
        if not self._rows:
            return None
        else:
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

    def _process_metadata(self, response: Dict[str, Any]) -> None:
        result_set = response.get("ResultSet")
        if not result_set:
            raise DataError("KeyError `ResultSet`")
        metadata = result_set.get("ResultSetMetadata")
        if not metadata:
            raise DataError("KeyError `ResultSetMetadata`")
        column_info = metadata.get("ColumnInfo")
        if column_info is None:
            raise DataError("KeyError `ColumnInfo`")
        self._metadata = tuple(column_info)

    def _process_update_count(self, response: Dict[str, Any]) -> None:
        update_count = response.get("UpdateCount")
        if (
            update_count is not None
            and self.substatement_type
            and self.substatement_type.upper()
            in (
                "INSERT",
                "UPDATE",
                "DELETE",
                "MERGE",
                "CREATE_TABLE_AS_SELECT",
            )
        ):
            self._rowcount = update_count

    def _get_rows(
        self, offset: int, metadata: Tuple[Any, ...], rows: List[Dict[str, Any]]
    ) -> List[Union[Tuple[Optional[Any], ...], Dict[Any, Optional[Any]]]]:
        return [
            tuple(
                [
                    self._converter.convert(meta.get("Type"), row.get("VarCharValue"))
                    for meta, row in zip(metadata, rows[i].get("Data", []))
                ]
            )
            for i in range(offset, len(rows))
        ]

    def _process_rows(self, response: Dict[str, Any]) -> None:
        result_set = response.get("ResultSet")
        if not result_set:
            raise DataError("KeyError `ResultSet`")
        rows = result_set.get("Rows")
        if rows is None:
            raise DataError("KeyError `Rows`")
        processed_rows = []
        if len(rows) > 0:
            offset = 1 if not self._next_token and self._is_first_row_column_labels(rows) else 0
            metadata = cast(Tuple[Any, ...], self._metadata)
            processed_rows = self._get_rows(offset, metadata, rows)
        self._rows.extend(processed_rows)
        self._next_token = response.get("NextToken")

    def _is_first_row_column_labels(self, rows: List[Dict[str, Any]]) -> bool:
        first_row_data = rows[0].get("Data", [])
        metadata = cast(Tuple[Any, Any], self._metadata)
        for meta, data in zip(metadata, first_row_data):
            if meta.get("Name") != data.get("VarCharValue"):
                return False
        return True

    def _get_content_length(self) -> int:
        if not self.output_location:
            raise ProgrammingError("OutputLocation is none or empty.")
        bucket, key = parse_output_location(self.output_location)
        try:
            response = retry_api_call(
                self._client.head_object,
                config=self._retry_config,
                logger=_logger,
                Bucket=bucket,
                Key=key,
            )
        except Exception as e:
            _logger.exception("Failed to get content length.")
            raise OperationalError(*e.args) from e
        else:
            return cast(int, response["ContentLength"])

    def _read_data_manifest(self) -> List[str]:
        if not self.data_manifest_location:
            raise ProgrammingError("DataManifestLocation is none or empty.")
        bucket, key = parse_output_location(self.data_manifest_location)
        try:
            response = retry_api_call(
                self._client.get_object,
                config=self._retry_config,
                logger=_logger,
                Bucket=bucket,
                Key=key,
            )
        except Exception as e:
            _logger.exception(f"Failed to read {bucket}/{key}.")
            raise OperationalError(*e.args) from e
        else:
            manifest: str = response["Body"].read().decode("utf-8").strip()
            return manifest.split("\n") if manifest else []

    @property
    def is_closed(self) -> bool:
        return self._connection is None

    def close(self) -> None:
        self._connection = None
        self._query_execution = None
        self._metadata = None
        self._rows.clear()
        self._next_token = None
        self._rownumber = None
        self._rowcount = -1

    def __enter__(self):
        return self

    def __exit__(self, exc_type, exc_val, exc_tb):
        self.close()


class AthenaDictResultSet(AthenaResultSet):
    # You can override this to use OrderedDict or other dict-like types.
    dict_type: Type[Any] = dict

    def _get_rows(
        self, offset: int, metadata: Tuple[Any, ...], rows: List[Dict[str, Any]]
    ) -> List[Union[Tuple[Optional[Any], ...], Dict[Any, Optional[Any]]]]:
        return [
            self.dict_type(
                [
                    (
                        meta.get("Name"),
                        self._converter.convert(meta.get("Type"), row.get("VarCharValue")),
                    )
                    for meta, row in zip(metadata, rows[i].get("Data", []))
                ]
            )
            for i in range(offset, len(rows))
        ]


class WithResultSet:
    def __init__(self):
        super().__init__()

    def _reset_state(self) -> None:
        self.query_id = None  # type: ignore
        if self.result_set and not self.result_set.is_closed:
            self.result_set.close()
        self.result_set = None  # type: ignore

    @property  # type: ignore
    @abstractmethod
    def result_set(self) -> Optional[AthenaResultSet]:
        raise NotImplementedError  # pragma: no cover

    @result_set.setter  # type: ignore
    @abstractmethod
    def result_set(self, val: Optional[AthenaResultSet]) -> None:
        raise NotImplementedError  # pragma: no cover

    @property
    def has_result_set(self) -> bool:
        return self.result_set is not None

    @property
    def description(
        self,
    ) -> Optional[List[Tuple[str, str, None, None, int, int, str]]]:
        if not self.result_set:
            return None
        return self.result_set.description

    @property
    def database(self) -> Optional[str]:
        if not self.result_set:
            return None
        return self.result_set.database

    @property
    def catalog(self) -> Optional[str]:
        if not self.result_set:
            return None
        return self.result_set.catalog

    @property  # type: ignore
    @abstractmethod
    def query_id(self) -> Optional[str]:
        raise NotImplementedError  # pragma: no cover

    @query_id.setter  # type: ignore
    @abstractmethod
    def query_id(self, val: Optional[str]) -> None:
        raise NotImplementedError  # pragma: no cover

    @property
    def query(self) -> Optional[str]:
        if not self.result_set:
            return None
        return self.result_set.query

    @property
    def statement_type(self) -> Optional[str]:
        if not self.result_set:
            return None
        return self.result_set.statement_type

    @property
    def substatement_type(self) -> Optional[str]:
        if not self.result_set:
            return None
        return self.result_set.substatement_type

    @property
    def work_group(self) -> Optional[str]:
        if not self.result_set:
            return None
        return self.result_set.work_group

    @property
    def execution_parameters(self) -> List[str]:
        if not self.result_set:
            return []
        return self.result_set.execution_parameters

    @property
    def state(self) -> Optional[str]:
        if not self.result_set:
            return None
        return self.result_set.state

    @property
    def state_change_reason(self) -> Optional[str]:
        if not self.result_set:
            return None
        return self.result_set.state_change_reason

    @property
    def submission_date_time(self) -> Optional[datetime]:
        if not self.result_set:
            return None
        return self.result_set.submission_date_time

    @property
    def completion_date_time(self) -> Optional[datetime]:
        if not self.result_set:
            return None
        return self.result_set.completion_date_time

    @property
    def error_category(self) -> Optional[int]:
        if not self.result_set:
            return None
        return self.result_set.error_category

    @property
    def error_type(self) -> Optional[int]:
        if not self.result_set:
            return None
        return self.result_set.error_type

    @property
    def retryable(self) -> Optional[bool]:
        if not self.result_set:
            return None
        return self.result_set.retryable

    @property
    def error_message(self) -> Optional[str]:
        if not self.result_set:
            return None
        return self.result_set.error_message

    @property
    def data_scanned_in_bytes(self) -> Optional[int]:
        if not self.result_set:
            return None
        return self.result_set.data_scanned_in_bytes

    @property
    def engine_execution_time_in_millis(self) -> Optional[int]:
        if not self.result_set:
            return None
        return self.result_set.engine_execution_time_in_millis

    @property
    def query_queue_time_in_millis(self) -> Optional[int]:
        if not self.result_set:
            return None
        return self.result_set.query_queue_time_in_millis

    @property
    def total_execution_time_in_millis(self) -> Optional[int]:
        if not self.result_set:
            return None
        return self.result_set.total_execution_time_in_millis

    @property
    def query_planning_time_in_millis(self) -> Optional[int]:
        if not self.result_set:
            return None
        return self.result_set.query_planning_time_in_millis

    @property
    def service_processing_time_in_millis(self) -> Optional[int]:
        if not self.result_set:
            return None
        return self.result_set.service_processing_time_in_millis

    @property
    def output_location(self) -> Optional[str]:
        if not self.result_set:
            return None
        return self.result_set.output_location

    @property
    def data_manifest_location(self) -> Optional[str]:
        if not self.result_set:
            return None
        return self.result_set.data_manifest_location

    @property
    def reused_previous_result(self) -> Optional[bool]:
        if not self.result_set:
            return None
        return self.result_set.reused_previous_result

    @property
    def encryption_option(self) -> Optional[str]:
        if not self.result_set:
            return None
        return self.result_set.encryption_option

    @property
    def kms_key(self) -> Optional[str]:
        if not self.result_set:
            return None
        return self.result_set.kms_key

    @property
    def expected_bucket_owner(self) -> Optional[str]:
        if not self.result_set:
            return None
        return self.result_set.expected_bucket_owner

    @property
    def s3_acl_option(self) -> Optional[str]:
        if not self.result_set:
            return None
        return self.result_set.s3_acl_option

    @property
    def selected_engine_version(self) -> Optional[str]:
        if not self.result_set:
            return None
        return self.result_set.selected_engine_version

    @property
    def effective_engine_version(self) -> Optional[str]:
        if not self.result_set:
            return None
        return self.result_set.effective_engine_version

    @property
    def result_reuse_enabled(self) -> Optional[bool]:
        if not self.result_set:
            return None
        return self.result_set.result_reuse_enabled

    @property
    def result_reuse_minutes(self) -> Optional[int]:
        if not self.result_set:
            return None
        return self.result_set.result_reuse_minutes

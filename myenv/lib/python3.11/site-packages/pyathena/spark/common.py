# -*- coding: utf-8 -*-
from __future__ import annotations

import logging
import time
from abc import ABCMeta, abstractmethod
from datetime import datetime
from typing import Any, Dict, List, Optional, Union, cast

import botocore

from pyathena import NotSupportedError, OperationalError
from pyathena.common import BaseCursor
from pyathena.model import (
    AthenaCalculationExecution,
    AthenaCalculationExecutionStatus,
    AthenaQueryExecution,
    AthenaSessionStatus,
)
from pyathena.util import parse_output_location, retry_api_call

_logger = logging.getLogger(__name__)  # type: ignore


class SparkBaseCursor(BaseCursor, metaclass=ABCMeta):
    def __init__(
        self,
        session_id: Optional[str] = None,
        description: Optional[str] = None,
        engine_configuration: Optional[Dict[str, Any]] = None,
        notebook_version: Optional[str] = None,
        session_idle_timeout_minutes: Optional[int] = None,
        **kwargs,
    ) -> None:
        super().__init__(**kwargs)
        self._engine_configuration = (
            engine_configuration
            if engine_configuration
            else self.get_default_engine_configuration()
        )
        self._notebook_version = notebook_version
        self._session_description = description
        self._session_idle_timeout_minutes = session_idle_timeout_minutes

        if session_id:
            if self._exists_session(session_id):
                self._session_id = session_id
            else:
                raise OperationalError(f"Session: {session_id} not found.")
        else:
            self._session_id = self._start_session()

        self._calculation_id: Optional[str] = None
        self._calculation_execution: Optional[AthenaCalculationExecution] = None

        self._client = self.connection.session.client(
            "s3",
            region_name=self.connection.region_name,
            config=self.connection.config,
            **self.connection._client_kwargs,
        )

    @property
    def session_id(self) -> str:
        return self._session_id

    @property
    def calculation_id(self) -> Optional[str]:
        return self._calculation_id

    @staticmethod
    def get_default_engine_configuration() -> Dict[str, Any]:
        return {
            "CoordinatorDpuSize": 1,
            "MaxConcurrentDpus": 2,
            "DefaultExecutorDpuSize": 1,
        }

    def _read_s3_file_as_text(self, uri) -> str:
        bucket, key = parse_output_location(uri)
        response = retry_api_call(
            self._client.get_object,
            config=self._retry_config,
            logger=_logger,
            Bucket=bucket,
            Key=key,
        )
        return cast(str, response["Body"].read().decode("utf-8").strip())

    def _get_session_status(self, session_id: str):
        request: Dict[str, Any] = {"SessionId": session_id}
        try:
            response = retry_api_call(
                self._connection.client.get_session_status,
                config=self._retry_config,
                logger=_logger,
                **request,
            )
        except Exception as e:
            _logger.exception("Failed to get session status.")
            raise OperationalError(*e.args) from e
        else:
            return AthenaSessionStatus(response)

    def _wait_for_idle_session(self, session_id: str):
        while True:
            session_status = self._get_session_status(session_id)
            if session_status.state in [AthenaSessionStatus.STATE_IDLE]:
                break
            elif session_status in [
                AthenaSessionStatus.STATE_TERMINATED,
                AthenaSessionStatus.STATE_DEGRADED,
                AthenaSessionStatus.STATE_FAILED,
            ]:
                raise OperationalError(session_status.state_change_reason)
            else:
                time.sleep(self._poll_interval)

    def _exists_session(self, session_id: str) -> bool:
        request = {"SessionId": session_id}
        try:
            retry_api_call(
                self._connection.client.get_session,
                config=self._retry_config,
                logger=_logger,
                **request,
            )
        except Exception as e:
            if (
                isinstance(e, botocore.exceptions.ClientError)
                and e.response["Error"]["Code"] == "InvalidRequestException"
            ):
                _logger.exception(f"Session: {session_id} not found.")
                return False
            else:
                raise OperationalError(*e.args) from e
        else:
            self._wait_for_idle_session(session_id)
            return True

    def _start_session(self) -> str:
        request: Dict[str, Any] = {
            "WorkGroup": self._work_group,
            "EngineConfiguration": self._engine_configuration,
        }
        if self._session_description:
            request.update({"Description": self._session_description})
        if self._notebook_version:
            request.update({"NotebookVersion": self._notebook_version})
        if self._session_idle_timeout_minutes:
            request.update({"SessionIdleTimeoutInMinutes": self._session_idle_timeout_minutes})
        try:
            session_id: str = retry_api_call(
                self._connection.client.start_session,
                config=self._retry_config,
                logger=_logger,
                **request,
            )["SessionId"]
        except Exception as e:
            _logger.exception("Failed to start session.")
            raise OperationalError(*e.args) from e
        else:
            self._wait_for_idle_session(session_id)
            return session_id

    def _terminate_session(self) -> None:
        request = {"SessionId": self._session_id}
        try:
            retry_api_call(
                self._connection.client.terminate_session,
                config=self._retry_config,
                logger=_logger,
                **request,
            )
        except Exception as e:
            _logger.exception("Failed to terminate session.")
            raise OperationalError(*e.args) from e

    def __poll(self, query_id: str) -> Union[AthenaQueryExecution, AthenaCalculationExecution]:
        while True:
            calculation_status = self._get_calculation_execution_status(query_id)
            if calculation_status.state in [
                AthenaCalculationExecutionStatus.STATE_COMPLETED,
                AthenaCalculationExecutionStatus.STATE_FAILED,
                AthenaCalculationExecutionStatus.STATE_CANCELED,
            ]:
                return self._get_calculation_execution(query_id)
            else:
                time.sleep(self._poll_interval)

    def _poll(self, query_id: str) -> Union[AthenaQueryExecution, AthenaCalculationExecution]:
        try:
            query_execution = self.__poll(query_id)
        except KeyboardInterrupt as e:
            if self._kill_on_interrupt:
                _logger.warning("Query canceled by user.")
                self._cancel(query_id)
                query_execution = self.__poll(query_id)
            else:
                raise e
        return query_execution

    def _cancel(self, query_id: str) -> None:
        request = {"CalculationExecutionId": query_id}
        try:
            retry_api_call(
                self._connection.client.stop_calculation_execution,
                config=self._retry_config,
                logger=_logger,
                **request,
            )
        except Exception as e:
            _logger.exception("Failed to cancel calculation.")
            raise OperationalError(*e.args) from e

    def close(self) -> None:
        self._terminate_session()

    def executemany(
        self, operation: str, seq_of_parameters: List[Optional[Dict[str, Any]]], **kwargs
    ) -> None:
        raise NotSupportedError


class WithCalculationExecution:
    def __init__(self):
        super().__init__()

    @property
    @abstractmethod
    def calculation_execution(self) -> Optional[AthenaCalculationExecution]:
        raise NotImplementedError  # pragma: no cover

    @property
    @abstractmethod
    def session_id(self) -> str:
        raise NotImplementedError  # pragma: no cover

    @property
    @abstractmethod
    def calculation_id(self) -> Optional[str]:
        raise NotImplementedError  # pragma: no cover

    @property
    def description(self) -> Optional[str]:
        if not self.calculation_execution:
            return None
        return self.calculation_execution.description

    @property
    def working_directory(self) -> Optional[str]:
        if not self.calculation_execution:
            return None
        return self.calculation_execution.working_directory

    @property
    def state(self) -> Optional[str]:
        if not self.calculation_execution:
            return None
        return self.calculation_execution.state

    @property
    def state_change_reason(self) -> Optional[str]:
        if not self.calculation_execution:
            return None
        return self.calculation_execution.state_change_reason

    @property
    def submission_date_time(self) -> Optional[datetime]:
        if not self.calculation_execution:
            return None
        return self.calculation_execution.submission_date_time

    @property
    def completion_date_time(self) -> Optional[datetime]:
        if not self.calculation_execution:
            return None
        return self.calculation_execution.completion_date_time

    @property
    def dpu_execution_in_millis(self) -> Optional[int]:
        if not self.calculation_execution:
            return None
        return self.calculation_execution.dpu_execution_in_millis

    @property
    def progress(self) -> Optional[str]:
        if not self.calculation_execution:
            return None
        return self.calculation_execution.progress

    @property
    def std_out_s3_uri(self) -> Optional[str]:
        if not self.calculation_execution:
            return None
        return self.calculation_execution.std_out_s3_uri

    @property
    def std_error_s3_uri(self) -> Optional[str]:
        if not self.calculation_execution:
            return None
        return self.calculation_execution.std_error_s3_uri

    @property
    def result_s3_uri(self) -> Optional[str]:
        if not self.calculation_execution:
            return None
        return self.calculation_execution.result_s3_uri

    @property
    def result_type(self) -> Optional[str]:
        if not self.calculation_execution:
            return None
        return self.calculation_execution.result_type

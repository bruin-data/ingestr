# -*- coding: utf-8 -*-
from __future__ import annotations

import logging
from typing import Any, Dict, Optional, cast

from pyathena import OperationalError, ProgrammingError
from pyathena.model import AthenaCalculationExecution, AthenaCalculationExecutionStatus
from pyathena.spark.common import SparkBaseCursor, WithCalculationExecution

_logger = logging.getLogger(__name__)  # type: ignore


class SparkCursor(SparkBaseCursor, WithCalculationExecution):
    def __init__(
        self,
        session_id: Optional[str] = None,
        description: Optional[str] = None,
        engine_configuration: Optional[Dict[str, Any]] = None,
        notebook_version: Optional[str] = None,
        session_idle_timeout_minutes: Optional[int] = None,
        **kwargs,
    ) -> None:
        super().__init__(
            session_id=session_id,
            description=description,
            engine_configuration=engine_configuration,
            notebook_version=notebook_version,
            session_idle_timeout_minutes=session_idle_timeout_minutes,
            **kwargs,
        )

    @property
    def calculation_execution(self) -> Optional[AthenaCalculationExecution]:
        return self._calculation_execution

    def get_std_out(self) -> Optional[str]:
        if not self._calculation_execution or not self._calculation_execution.std_out_s3_uri:
            return None
        return self._read_s3_file_as_text(self._calculation_execution.std_out_s3_uri)

    def get_std_error(self) -> Optional[str]:
        if not self._calculation_execution or not self._calculation_execution.std_error_s3_uri:
            return None
        return self._read_s3_file_as_text(self._calculation_execution.std_error_s3_uri)

    def execute(
        self,
        operation: str,
        parameters: Optional[Dict[str, Any]] = None,
        session_id: Optional[str] = None,
        description: Optional[str] = None,
        client_request_token: Optional[str] = None,
        work_group: Optional[str] = None,
        **kwargs,
    ) -> SparkCursor:
        self._calculation_id = self._calculate(
            session_id=session_id if session_id else self._session_id,
            code_block=operation,
            description=description,
            client_request_token=client_request_token,
        )
        self._calculation_execution = cast(
            AthenaCalculationExecution, self._poll(self._calculation_id)
        )
        if self._calculation_execution.state != AthenaCalculationExecutionStatus.STATE_COMPLETED:
            std_error = self.get_std_error()
            raise OperationalError(std_error)
        return self

    def cancel(self) -> None:
        if not self.calculation_id:
            raise ProgrammingError("CalculationExecutionId is none or empty.")
        self._cancel(self.calculation_id)

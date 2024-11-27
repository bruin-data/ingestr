# -*- coding: utf-8 -*-
import logging
from concurrent.futures import Future, ThreadPoolExecutor
from multiprocessing import cpu_count
from typing import TYPE_CHECKING, Any, Dict, Optional, Tuple, Union, cast

from pyathena.model import AthenaCalculationExecution
from pyathena.spark.common import SparkBaseCursor

if TYPE_CHECKING:
    from pyathena.model import AthenaQueryExecution

_logger = logging.getLogger(__name__)  # type: ignore


class AsyncSparkCursor(SparkBaseCursor):
    def __init__(
        self,
        session_id: Optional[str] = None,
        description: Optional[str] = None,
        engine_configuration: Optional[Dict[str, Any]] = None,
        notebook_version: Optional[str] = None,
        session_idle_timeout_minutes: Optional[int] = None,
        max_workers: int = (cpu_count() or 1) * 5,
        **kwargs,
    ):
        super().__init__(
            session_id=session_id,
            description=description,
            engine_configuration=engine_configuration,
            notebook_version=notebook_version,
            session_idle_timeout_minutes=session_idle_timeout_minutes,
            **kwargs,
        )
        self._max_workers = max_workers
        self._executor = ThreadPoolExecutor(max_workers=max_workers)

    def close(self, wait: bool = False) -> None:
        super().close()
        self._executor.shutdown(wait=wait)

    def calculation_execution(self, query_id: str) -> "Future[AthenaCalculationExecution]":
        return self._executor.submit(self._get_calculation_execution, query_id)

    def get_std_out(
        self, calculation_execution: AthenaCalculationExecution
    ) -> "Optional[Future[str]]":
        if not calculation_execution.std_out_s3_uri:
            return None
        return self._executor.submit(
            self._read_s3_file_as_text, calculation_execution.std_out_s3_uri
        )

    def get_std_error(
        self, calculation_execution: AthenaCalculationExecution
    ) -> "Optional[Future[str]]":
        if not calculation_execution.std_error_s3_uri:
            return None
        return self._executor.submit(
            self._read_s3_file_as_text, calculation_execution.std_error_s3_uri
        )

    def poll(self, query_id: str) -> "Future[AthenaCalculationExecution]":
        return cast(
            "Future[AthenaCalculationExecution]", self._executor.submit(self._poll, query_id)
        )

    def execute(
        self,
        operation: str,
        parameters: Optional[Dict[str, Any]] = None,
        session_id: Optional[str] = None,
        description: Optional[str] = None,
        client_request_token: Optional[str] = None,
        work_group: Optional[str] = None,
        **kwargs,
    ) -> Tuple[str, "Future[Union[AthenaQueryExecution, AthenaCalculationExecution]]"]:
        calculation_id = self._calculate(
            session_id=session_id if session_id else self._session_id,
            code_block=operation,
            description=description,
            client_request_token=client_request_token,
        )
        return calculation_id, self._executor.submit(self._poll, calculation_id)

    def cancel(self, query_id: str) -> "Future[None]":
        return self._executor.submit(self._cancel, query_id)

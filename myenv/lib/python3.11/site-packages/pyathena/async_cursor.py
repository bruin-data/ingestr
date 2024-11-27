# -*- coding: utf-8 -*-
from __future__ import annotations

import logging
from concurrent.futures import Future
from concurrent.futures.thread import ThreadPoolExecutor
from multiprocessing import cpu_count
from typing import Any, Dict, List, Optional, Tuple, Union, cast

from pyathena.common import CursorIterator
from pyathena.cursor import BaseCursor
from pyathena.error import NotSupportedError, ProgrammingError
from pyathena.model import AthenaQueryExecution
from pyathena.result_set import AthenaDictResultSet, AthenaResultSet

_logger = logging.getLogger(__name__)  # type: ignore


class AsyncCursor(BaseCursor):
    def __init__(
        self,
        s3_staging_dir: Optional[str] = None,
        schema_name: Optional[str] = None,
        catalog_name: Optional[str] = None,
        work_group: Optional[str] = None,
        poll_interval: float = 1,
        encryption_option: Optional[str] = None,
        kms_key: Optional[str] = None,
        kill_on_interrupt: bool = True,
        max_workers: int = (cpu_count() or 1) * 5,
        arraysize: int = CursorIterator.DEFAULT_FETCH_SIZE,
        result_reuse_enable: bool = False,
        result_reuse_minutes: int = CursorIterator.DEFAULT_RESULT_REUSE_MINUTES,
        **kwargs,
    ) -> None:
        super().__init__(
            s3_staging_dir=s3_staging_dir,
            schema_name=schema_name,
            catalog_name=catalog_name,
            work_group=work_group,
            poll_interval=poll_interval,
            encryption_option=encryption_option,
            kms_key=kms_key,
            kill_on_interrupt=kill_on_interrupt,
            result_reuse_enable=result_reuse_enable,
            result_reuse_minutes=result_reuse_minutes,
            **kwargs,
        )
        self._max_workers = max_workers
        self._executor = ThreadPoolExecutor(max_workers=max_workers)
        self._arraysize = arraysize
        self._result_set_class = AthenaResultSet

    @property
    def arraysize(self) -> int:
        return self._arraysize

    @arraysize.setter
    def arraysize(self, value: int) -> None:
        if value <= 0 or value > CursorIterator.DEFAULT_FETCH_SIZE:
            raise ProgrammingError(
                "MaxResults is more than maximum allowed length "
                f"{CursorIterator.DEFAULT_FETCH_SIZE}."
            )
        self._arraysize = value

    def close(self, wait: bool = False) -> None:
        self._executor.shutdown(wait=wait)

    def _description(
        self, query_id: str
    ) -> Optional[List[Tuple[str, str, None, None, int, int, str]]]:
        result_set = self._collect_result_set(query_id)
        return result_set.description

    def description(
        self, query_id: str
    ) -> "Future[Optional[List[Tuple[str, str, None, None, int, int, str]]]]":
        return self._executor.submit(self._description, query_id)

    def query_execution(self, query_id: str) -> "Future[AthenaQueryExecution]":
        return self._executor.submit(self._get_query_execution, query_id)

    def poll(self, query_id: str) -> "Future[AthenaQueryExecution]":
        return cast("Future[AthenaQueryExecution]", self._executor.submit(self._poll, query_id))

    def _collect_result_set(self, query_id: str) -> AthenaResultSet:
        query_execution = cast(AthenaQueryExecution, self._poll(query_id))
        return self._result_set_class(
            connection=self._connection,
            converter=self._converter,
            query_execution=query_execution,
            arraysize=self._arraysize,
            retry_config=self._retry_config,
        )

    def execute(
        self,
        operation: str,
        parameters: Optional[Dict[str, Any]] = None,
        work_group: Optional[str] = None,
        s3_staging_dir: Optional[str] = None,
        cache_size: Optional[int] = 0,
        cache_expiration_time: Optional[int] = 0,
        result_reuse_enable: Optional[bool] = None,
        result_reuse_minutes: Optional[int] = None,
        **kwargs,
    ) -> Tuple[str, "Future[Union[AthenaResultSet, Any]]"]:
        query_id = self._execute(
            operation,
            parameters=parameters,
            work_group=work_group,
            s3_staging_dir=s3_staging_dir,
            cache_size=cache_size,
            cache_expiration_time=cache_expiration_time,
            result_reuse_enable=result_reuse_enable,
            result_reuse_minutes=result_reuse_minutes,
        )
        return query_id, self._executor.submit(self._collect_result_set, query_id)

    def executemany(
        self, operation: str, seq_of_parameters: List[Optional[Dict[str, Any]]], **kwargs
    ) -> None:
        raise NotSupportedError

    def cancel(self, query_id: str) -> "Future[None]":
        return self._executor.submit(self._cancel, query_id)


class AsyncDictCursor(AsyncCursor):
    def __init__(self, **kwargs) -> None:
        super().__init__(**kwargs)
        self._result_set_class = AthenaDictResultSet
        if "dict_type" in kwargs:
            AthenaDictResultSet.dict_type = kwargs["dict_type"]

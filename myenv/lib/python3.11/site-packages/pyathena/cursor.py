# -*- coding: utf-8 -*-
from __future__ import annotations

import logging
from typing import Any, Dict, List, Optional, Tuple, Union, cast

from pyathena.common import BaseCursor, CursorIterator
from pyathena.error import OperationalError, ProgrammingError
from pyathena.model import AthenaQueryExecution
from pyathena.result_set import AthenaDictResultSet, AthenaResultSet, WithResultSet

_logger = logging.getLogger(__name__)  # type: ignore


class Cursor(BaseCursor, CursorIterator, WithResultSet):
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
        self._query_id: Optional[str] = None
        self._result_set: Optional[AthenaResultSet] = None
        self._result_set_class = AthenaResultSet

    @property
    def result_set(self) -> Optional[AthenaResultSet]:
        return self._result_set

    @result_set.setter
    def result_set(self, val) -> None:
        self._result_set = val

    @property
    def query_id(self) -> Optional[str]:
        return self._query_id

    @query_id.setter
    def query_id(self, val) -> None:
        self._query_id = val

    @property
    def rownumber(self) -> Optional[int]:
        return self.result_set.rownumber if self.result_set else None

    @property
    def rowcount(self) -> int:
        return self.result_set.rowcount if self.result_set else -1

    def close(self) -> None:
        if self.result_set and not self.result_set.is_closed:
            self.result_set.close()

    def execute(
        self,
        operation: str,
        parameters: Optional[Dict[str, Any]] = None,
        work_group: Optional[str] = None,
        s3_staging_dir: Optional[str] = None,
        cache_size: int = 0,
        cache_expiration_time: int = 0,
        result_reuse_enable: Optional[bool] = None,
        result_reuse_minutes: Optional[int] = None,
        **kwargs,
    ) -> Cursor:
        self._reset_state()
        self.query_id = self._execute(
            operation,
            parameters=parameters,
            work_group=work_group,
            s3_staging_dir=s3_staging_dir,
            cache_size=cache_size,
            cache_expiration_time=cache_expiration_time,
            result_reuse_enable=result_reuse_enable,
            result_reuse_minutes=result_reuse_minutes,
        )
        query_execution = cast(AthenaQueryExecution, self._poll(self.query_id))
        if query_execution.state == AthenaQueryExecution.STATE_SUCCEEDED:
            self.result_set = self._result_set_class(
                self._connection,
                self._converter,
                query_execution,
                self.arraysize,
                self._retry_config,
            )
        else:
            raise OperationalError(query_execution.state_change_reason)
        return self

    def executemany(
        self, operation: str, seq_of_parameters: List[Optional[Dict[str, Any]]], **kwargs
    ) -> None:
        for parameters in seq_of_parameters:
            self.execute(operation, parameters)
        # Operations that have result sets are not allowed with executemany.
        self._reset_state()

    def cancel(self) -> None:
        if not self.query_id:
            raise ProgrammingError("QueryExecutionId is none or empty.")
        self._cancel(self.query_id)

    def fetchone(
        self,
    ) -> Optional[Union[Tuple[Optional[Any], ...], Dict[Any, Optional[Any]]]]:
        if not self.has_result_set:
            raise ProgrammingError("No result set.")
        result_set = cast(AthenaResultSet, self.result_set)
        return result_set.fetchone()

    def fetchmany(
        self, size: Optional[int] = None
    ) -> List[Union[Tuple[Optional[Any], ...], Dict[Any, Optional[Any]]]]:
        if not self.has_result_set:
            raise ProgrammingError("No result set.")
        result_set = cast(AthenaResultSet, self.result_set)
        return result_set.fetchmany(size)

    def fetchall(
        self,
    ) -> List[Union[Tuple[Optional[Any], ...], Dict[Any, Optional[Any]]]]:
        if not self.has_result_set:
            raise ProgrammingError("No result set.")
        result_set = cast(AthenaResultSet, self.result_set)
        return result_set.fetchall()


class DictCursor(Cursor):
    def __init__(self, **kwargs) -> None:
        super().__init__(**kwargs)
        self._result_set_class = AthenaDictResultSet
        if "dict_type" in kwargs:
            AthenaDictResultSet.dict_type = kwargs["dict_type"]

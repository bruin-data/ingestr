#!/usr/bin/env python
#
# Copyright (c) 2012-2023 Snowflake Computing Inc. All rights reserved.
#

from __future__ import annotations

import uuid
from io import BytesIO
from logging import getLogger
from typing import TYPE_CHECKING

from ._utils import (
    _PYTHON_SNOWPARK_USE_SCOPED_TEMP_OBJECTS_STRING,
    get_temp_type_for_object,
)
from .errors import BindUploadError, Error

if TYPE_CHECKING:  # pragma: no cover
    from .cursor import SnowflakeCursor

logger = getLogger(__name__)


class BindUploadAgent:

    def __init__(
        self,
        cursor: SnowflakeCursor,
        rows: list[bytes],
        stream_buffer_size: int = 1024 * 1024 * 10,
    ) -> None:
        """Construct an agent that uploads binding parameters as CSV files to a temporary stage.

        Args:
            cursor: The cursor object.
            rows: Rows of binding parameters in CSV format.
            stream_buffer_size: Size of each file, default to 10MB.
        """
        self._use_scoped_temp_object = (
            cursor.connection._session_parameters.get(
                _PYTHON_SNOWPARK_USE_SCOPED_TEMP_OBJECTS_STRING, False
            )
            if cursor.connection._session_parameters
            else False
        )
        self._STAGE_NAME = (
            "SNOWPARK_TEMP_STAGE_BIND" if self._use_scoped_temp_object else "SYSTEMBIND"
        )
        self.cursor = cursor
        self.rows = rows
        self._stream_buffer_size = stream_buffer_size
        self.stage_path = f"@{self._STAGE_NAME}/{uuid.uuid4().hex}"

    def _create_stage(self) -> None:
        create_stage_sql = (
            f"create or replace {get_temp_type_for_object(self._use_scoped_temp_object)} stage {self._STAGE_NAME} "
            "file_format=(type=csv field_optionally_enclosed_by='\"')"
        )
        self.cursor.execute(create_stage_sql)

    def upload(self) -> None:
        try:
            self._create_stage()
        except Error as err:
            self.cursor.connection._session_parameters[
                "CLIENT_STAGE_ARRAY_BINDING_THRESHOLD"
            ] = 0
            logger.debug("Failed to create stage for binding.")
            raise BindUploadError from err

        row_idx = 0
        while row_idx < len(self.rows):
            f = BytesIO()
            size = 0
            while True:
                f.write(self.rows[row_idx])
                size += len(self.rows[row_idx])
                row_idx += 1
                if row_idx >= len(self.rows) or size >= self._stream_buffer_size:
                    break
            try:
                self.cursor.execute(
                    f"PUT file://{row_idx}.csv {self.stage_path}", file_stream=f
                )
            except Error as err:
                logger.debug("Failed to upload the bindings file to stage.")
                raise BindUploadError from err
            f.close()

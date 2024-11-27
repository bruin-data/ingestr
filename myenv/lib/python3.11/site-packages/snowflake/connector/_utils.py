#
# Copyright (c) 2012-2023 Snowflake Computing Inc. All rights reserved.
#

from __future__ import annotations

import string
from enum import Enum
from random import choice
from threading import Timer


class TempObjectType(Enum):
    TABLE = "TABLE"
    VIEW = "VIEW"
    STAGE = "STAGE"
    FUNCTION = "FUNCTION"
    FILE_FORMAT = "FILE_FORMAT"
    QUERY_TAG = "QUERY_TAG"
    COLUMN = "COLUMN"
    PROCEDURE = "PROCEDURE"
    TABLE_FUNCTION = "TABLE_FUNCTION"
    DYNAMIC_TABLE = "DYNAMIC_TABLE"
    AGGREGATE_FUNCTION = "AGGREGATE_FUNCTION"
    CTE = "CTE"


TEMP_OBJECT_NAME_PREFIX = "SNOWPARK_TEMP_"
ALPHANUMERIC = string.digits + string.ascii_lowercase
TEMPORARY_STRING = "TEMP"
SCOPED_TEMPORARY_STRING = "SCOPED TEMPORARY"
_PYTHON_SNOWPARK_USE_SCOPED_TEMP_OBJECTS_STRING = (
    "PYTHON_SNOWPARK_USE_SCOPED_TEMP_OBJECTS"
)


def generate_random_alphanumeric(length: int = 10) -> str:
    return "".join(choice(ALPHANUMERIC) for _ in range(length))


def random_name_for_temp_object(object_type: TempObjectType) -> str:
    return f"{TEMP_OBJECT_NAME_PREFIX}{object_type.value}_{generate_random_alphanumeric().upper()}"


def get_temp_type_for_object(use_scoped_temp_objects: bool) -> str:
    return SCOPED_TEMPORARY_STRING if use_scoped_temp_objects else TEMPORARY_STRING


class _TrackedQueryCancellationTimer(Timer):
    def __init__(self, interval, function, args=None, kwargs=None):
        super().__init__(interval, function, args, kwargs)
        self.executed = False

    def run(self):
        super().run()
        self.executed = True

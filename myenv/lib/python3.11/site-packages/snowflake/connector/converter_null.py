#!/usr/bin/env python
#
# Copyright (c) 2012-2023 Snowflake Computing Inc. All rights reserved.
#

from __future__ import annotations

from typing import Any

from .converter import SnowflakeConverter


class SnowflakeNoConverterToPython(SnowflakeConverter):
    def __init__(self, **kwargs) -> None:
        super().__init__(**kwargs)

    def to_python_method(self, type_name: str, column: dict[str, Any]) -> None:
        return None

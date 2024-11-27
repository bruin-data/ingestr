# -*- coding: utf-8 -*-
from __future__ import annotations

from datetime import date, datetime
from typing import TYPE_CHECKING, Any, Optional, Union

from sqlalchemy.sql import sqltypes
from sqlalchemy.sql.type_api import TypeEngine

if TYPE_CHECKING:
    from sqlalchemy import Dialect
    from sqlalchemy.sql.type_api import _LiteralProcessorType


class AthenaTimestamp(TypeEngine[datetime]):
    render_literal_cast = True
    render_bind_cast = True

    @staticmethod
    def process(value: Optional[Union[datetime, Any]]) -> str:
        if isinstance(value, datetime):
            return f"""TIMESTAMP '{value.strftime("%Y-%m-%d %H:%M:%S.%f")[:-3]}'"""
        return f"TIMESTAMP '{str(value)}'"

    def literal_processor(self, dialect: "Dialect") -> Optional["_LiteralProcessorType[datetime]"]:
        return self.process


class AthenaDate(TypeEngine[date]):
    render_literal_cast = True
    render_bind_cast = True

    @staticmethod
    def process(value: Union[date, datetime, Any]) -> str:
        if isinstance(value, (date, datetime)):
            f"DATE '{value:%Y-%m-%d}'"
        return f"DATE '{str(value)}'"

    def literal_processor(self, dialect: "Dialect") -> Optional["_LiteralProcessorType[date]"]:
        return self.process


class Tinyint(sqltypes.Integer):
    __visit_name__ = "tinyint"


class TINYINT(Tinyint):
    __visit_name__ = "TINYINT"

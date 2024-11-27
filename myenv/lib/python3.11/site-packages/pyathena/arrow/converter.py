# -*- coding: utf-8 -*-
from __future__ import annotations

import logging
from builtins import isinstance
from copy import deepcopy
from datetime import date, datetime
from typing import Any, Callable, Dict, Optional, Type, Union

from pyathena.converter import (
    Converter,
    _to_binary,
    _to_decimal,
    _to_default,
    _to_json,
    _to_time,
)

_logger = logging.getLogger(__name__)  # type: ignore


def _to_date(value: Optional[Union[str, datetime]]) -> Optional[date]:
    if value is None:
        return None
    elif isinstance(value, datetime):
        return value.date()
    else:
        return datetime.strptime(value, "%Y-%m-%d").date()


_DEFAULT_ARROW_CONVERTERS: Dict[str, Callable[[Optional[str]], Optional[Any]]] = {
    "date": _to_date,
    "time": _to_time,
    "decimal": _to_decimal,
    "varbinary": _to_binary,
    "json": _to_json,
}


class DefaultArrowTypeConverter(Converter):
    def __init__(self) -> None:
        super().__init__(
            mappings=deepcopy(_DEFAULT_ARROW_CONVERTERS),
            default=_to_default,
            types=self._dtypes,
        )

    @property
    def _dtypes(self) -> Dict[str, Type[Any]]:
        if not hasattr(self, "__dtypes"):
            import pyarrow as pa

            self.__dtypes = {
                "boolean": pa.bool_(),
                "tinyint": pa.int8(),
                "smallint": pa.int16(),
                "integer": pa.int32(),
                "bigint": pa.int64(),
                "float": pa.float32(),
                "real": pa.float64(),
                "double": pa.float64(),
                "char": pa.string(),
                "varchar": pa.string(),
                "string": pa.string(),
                "timestamp": pa.timestamp("ms"),
                "date": pa.timestamp("ms"),
                "time": pa.string(),
                "varbinary": pa.string(),
                "array": pa.string(),
                "map": pa.string(),
                "row": pa.string(),
                "decimal": pa.string(),
                "json": pa.string(),
            }
        return self.__dtypes

    def convert(self, type_: str, value: Optional[str]) -> Optional[Any]:
        converter = self.get(type_)
        return converter(value)


class DefaultArrowUnloadTypeConverter(Converter):
    def __init__(self) -> None:
        super().__init__(
            mappings=dict(),
            default=_to_default,
        )

    def convert(self, type_: str, value: Optional[str]) -> Optional[Any]:
        pass

# -*- coding: utf-8 -*-
from __future__ import annotations

import logging
from copy import deepcopy
from typing import Any, Callable, Dict, Optional, Type

from pyathena.converter import (
    Converter,
    _to_binary,
    _to_boolean,
    _to_decimal,
    _to_default,
    _to_json,
)

_logger = logging.getLogger(__name__)  # type: ignore


_DEFAULT_PANDAS_CONVERTERS: Dict[str, Callable[[Optional[str]], Optional[Any]]] = {
    "boolean": _to_boolean,
    "decimal": _to_decimal,
    "varbinary": _to_binary,
    "json": _to_json,
}


class DefaultPandasTypeConverter(Converter):
    def __init__(self) -> None:
        super().__init__(
            mappings=deepcopy(_DEFAULT_PANDAS_CONVERTERS),
            default=_to_default,
            types=self._dtypes,
        )

    @property
    def _dtypes(self) -> Dict[str, Type[Any]]:
        if not hasattr(self, "__dtypes"):
            import pandas as pd

            self.__dtypes = {
                "tinyint": pd.Int64Dtype(),
                "smallint": pd.Int64Dtype(),
                "integer": pd.Int64Dtype(),
                "bigint": pd.Int64Dtype(),
                "float": float,
                "real": float,
                "double": float,
                "char": str,
                "varchar": str,
                "string": str,
                "array": str,
                "map": str,
                "row": str,
            }
        return self.__dtypes

    def convert(self, type_: str, value: Optional[str]) -> Optional[Any]:
        pass


class DefaultPandasUnloadTypeConverter(Converter):
    def __init__(self) -> None:
        super().__init__(
            mappings=dict(),
            default=_to_default,
        )

    def convert(self, type_: str, value: Optional[str]) -> Optional[Any]:
        pass

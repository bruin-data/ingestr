# -*- coding: utf-8 -*-
from __future__ import annotations

import logging
import textwrap
import uuid
from abc import ABCMeta, abstractmethod
from copy import deepcopy
from datetime import date, datetime, timezone
from decimal import Decimal
from typing import Any, Callable, Dict, Optional, Type

from pyathena.error import ProgrammingError
from pyathena.model import AthenaCompression, AthenaFileFormat

_logger = logging.getLogger(__name__)  # type: ignore


class Formatter(metaclass=ABCMeta):
    def __init__(
        self,
        mappings: Dict[Type[Any], Callable[[Formatter, Callable[[str], str], Any], Any]],
        default: Optional[Callable[[Formatter, Callable[[str], str], Any], Any]] = None,
    ) -> None:
        self._mappings = mappings
        self._default = default

    @property
    def mappings(
        self,
    ) -> Dict[Type[Any], Callable[[Formatter, Callable[[str], str], Any], Any]]:
        return self._mappings

    def get(self, type_) -> Optional[Callable[[Formatter, Callable[[str], str], Any], Any]]:
        return self.mappings.get(type(type_), self._default)

    def set(
        self,
        type_: Type[Any],
        formatter: Callable[[Formatter, Callable[[str], str], Any], Any],
    ) -> None:
        self.mappings[type_] = formatter

    def remove(self, type_: Type[Any]) -> None:
        self.mappings.pop(type_, None)

    def update(
        self, mappings: Dict[Type[Any], Callable[[Formatter, Callable[[str], str], Any], Any]]
    ) -> None:
        self.mappings.update(mappings)

    @abstractmethod
    def format(self, operation: str, parameters: Optional[Dict[str, Any]] = None) -> str:
        raise NotImplementedError  # pragma: no cover

    @staticmethod
    def wrap_unload(
        operation: str,
        s3_staging_dir: str,
        format_: str = AthenaFileFormat.FILE_FORMAT_PARQUET,
        compression: str = AthenaCompression.COMPRESSION_SNAPPY,
    ):
        if not operation or not operation.strip():
            raise ProgrammingError("Query is none or empty.")

        operation_upper = operation.strip().upper()
        if operation_upper.startswith("SELECT") or operation_upper.startswith("WITH"):
            now = datetime.now(timezone.utc).strftime("%Y%m%d")
            location = f"{s3_staging_dir}unload/{now}/{str(uuid.uuid4())}/"
            operation = textwrap.dedent(
                f"""
                UNLOAD (
                \t{operation.strip()}
                )
                TO '{location}'
                WITH (
                \tformat = '{format_}',
                \tcompression = '{compression}'
                )
                """
            )
        else:
            location = None
        return operation, location


def _escape_presto(val: str) -> str:
    escaped = val.replace("'", "''")
    return f"'{escaped}'"


def _escape_hive(val: str) -> str:
    escaped = (
        val.replace("\\", "\\\\")
        .replace("'", "\\'")
        .replace("\r", "\\r")
        .replace("\n", "\\n")
        .replace("\t", "\\t")
    )
    return f"'{escaped}'"


def _format_none(formatter: Formatter, escaper: Callable[[str], str], val: Any) -> Any:
    return "null"


def _format_default(formatter: Formatter, escaper: Callable[[str], str], val: Any) -> Any:
    return val


def _format_date(formatter: Formatter, escaper: Callable[[str], str], val: Any) -> Any:
    return f"DATE '{val:%Y-%m-%d}'"


def _format_datetime(formatter: Formatter, escaper: Callable[[str], str], val: Any) -> Any:
    return f"""TIMESTAMP '{val.strftime("%Y-%m-%d %H:%M:%S.%f")[:-3]}'"""


def _format_bool(formatter: Formatter, escaper: Callable[[str], str], val: Any) -> Any:
    return str(val)


def _format_str(formatter: Formatter, escaper: Callable[[str], str], val: Any) -> Any:
    return escaper(val)


def _format_seq(formatter: Formatter, escaper: Callable[[str], str], val: Any) -> Any:
    results = []
    for v in val:
        func = formatter.get(v)
        if not func:
            raise TypeError(f"{type(v)} is not defined formatter.")
        formatted = func(formatter, escaper, v)
        if not isinstance(
            formatted,
            (str,),
        ):
            # force string format
            if isinstance(
                formatted,
                (
                    float,
                    Decimal,
                ),
            ):
                formatted = f"{formatted:f}"
            else:
                formatted = f"{formatted}"
        results.append(formatted)
    return f"""({", ".join(results)})"""


def _format_decimal(formatter: Formatter, escaper: Callable[[str], str], val: Any) -> Any:
    escaped = escaper(f"{val:f}")
    return f"DECIMAL {escaped}"


_DEFAULT_FORMATTERS: Dict[Type[Any], Callable[[Formatter, Callable[[str], str], Any], Any]] = {
    type(None): _format_none,
    date: _format_date,
    datetime: _format_datetime,
    int: _format_default,
    float: _format_default,
    Decimal: _format_decimal,
    bool: _format_bool,
    str: _format_str,
    list: _format_seq,
    set: _format_seq,
    tuple: _format_seq,
}


class DefaultParameterFormatter(Formatter):
    def __init__(self) -> None:
        super().__init__(mappings=deepcopy(_DEFAULT_FORMATTERS), default=None)

    def format(self, operation: str, parameters: Optional[Dict[str, Any]] = None) -> str:
        if not operation or not operation.strip():
            raise ProgrammingError("Query is none or empty.")
        operation = operation.strip()

        operation_upper = operation.upper()
        if (
            operation_upper.startswith("SELECT")
            or operation_upper.startswith("WITH")
            or operation_upper.startswith("INSERT")
            or operation_upper.startswith("UPDATE")
            or operation_upper.startswith("MERGE")
        ):
            escaper = _escape_presto
        else:
            escaper = _escape_hive

        kwargs: Optional[Dict[str, Any]] = None
        if parameters is not None:
            kwargs = dict()
            if not parameters:
                pass
            elif isinstance(parameters, dict):
                for k, v in parameters.items():
                    func = self.get(v)
                    if not func:
                        raise TypeError(f"{type(v)} is not defined formatter.")
                    kwargs.update({k: func(self, escaper, v)})
            else:
                raise ProgrammingError(
                    f"Unsupported parameter (Support for dict only): {parameters}"
                )

        return (operation % kwargs).strip() if kwargs is not None else operation.strip()

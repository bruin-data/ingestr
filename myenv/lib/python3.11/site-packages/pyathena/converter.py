# -*- coding: utf-8 -*-
from __future__ import annotations

import binascii
import json
import logging
from abc import ABCMeta, abstractmethod
from copy import deepcopy
from datetime import date, datetime, time
from decimal import Decimal
from typing import Any, Callable, Dict, Optional, Type

from dateutil.tz import gettz

from pyathena.util import strtobool

_logger = logging.getLogger(__name__)  # type: ignore


def _to_date(varchar_value: Optional[str]) -> Optional[date]:
    if varchar_value is None:
        return None
    return datetime.strptime(varchar_value, "%Y-%m-%d").date()


def _to_datetime(varchar_value: Optional[str]) -> Optional[datetime]:
    if varchar_value is None:
        return None
    return datetime.strptime(varchar_value, "%Y-%m-%d %H:%M:%S.%f")


def _to_datetime_with_tz(varchar_value: Optional[str]) -> Optional[datetime]:
    if varchar_value is None:
        return None
    datetime_, _, tz = varchar_value.rpartition(" ")
    return datetime.strptime(datetime_, "%Y-%m-%d %H:%M:%S.%f").replace(tzinfo=gettz(tz))


def _to_time(varchar_value: Optional[str]) -> Optional[time]:
    if varchar_value is None:
        return None
    return datetime.strptime(varchar_value, "%H:%M:%S.%f").time()


def _to_float(varchar_value: Optional[str]) -> Optional[float]:
    if varchar_value is None:
        return None
    return float(varchar_value)


def _to_int(varchar_value: Optional[str]) -> Optional[int]:
    if varchar_value is None:
        return None
    return int(varchar_value)


def _to_decimal(varchar_value: Optional[str]) -> Optional[Decimal]:
    if not varchar_value:
        return None
    return Decimal(varchar_value)


def _to_boolean(varchar_value: Optional[str]) -> Optional[bool]:
    if not varchar_value:
        return None
    return bool(strtobool(varchar_value))


def _to_binary(varchar_value: Optional[str]) -> Optional[bytes]:
    if varchar_value is None:
        return None
    return binascii.a2b_hex("".join(varchar_value.split(" ")))


def _to_json(varchar_value: Optional[str]) -> Optional[Any]:
    if varchar_value is None:
        return None
    return json.loads(varchar_value)


def _to_default(varchar_value: Optional[str]) -> Optional[str]:
    return varchar_value


_DEFAULT_CONVERTERS: Dict[str, Callable[[Optional[str]], Optional[Any]]] = {
    "boolean": _to_boolean,
    "tinyint": _to_int,
    "smallint": _to_int,
    "integer": _to_int,
    "bigint": _to_int,
    "float": _to_float,
    "real": _to_float,
    "double": _to_float,
    "char": _to_default,
    "varchar": _to_default,
    "string": _to_default,
    "timestamp": _to_datetime,
    "timestamp with time zone": _to_datetime_with_tz,
    "date": _to_date,
    "time": _to_time,
    "varbinary": _to_binary,
    "array": _to_default,
    "map": _to_default,
    "row": _to_default,
    "decimal": _to_decimal,
    "json": _to_json,
}


class Converter(metaclass=ABCMeta):
    def __init__(
        self,
        mappings: Dict[str, Callable[[Optional[str]], Optional[Any]]],
        default: Callable[[Optional[str]], Optional[Any]] = _to_default,
        types: Optional[Dict[str, Type[Any]]] = None,
    ) -> None:
        if mappings:
            self._mappings = mappings
        else:
            self._mappings = dict()
        self._default = default
        if types:
            self._types = types
        else:
            self._types = dict()

    @property
    def mappings(self) -> Dict[str, Callable[[Optional[str]], Optional[Any]]]:
        return self._mappings

    @property
    def types(self) -> Dict[str, Type[Any]]:
        return self._types

    def get(self, type_: str) -> Callable[[Optional[str]], Optional[Any]]:
        return self.mappings.get(type_, self._default)

    def set(self, type_: str, converter: Callable[[Optional[str]], Optional[Any]]) -> None:
        self.mappings[type_] = converter

    def remove(self, type_: str) -> None:
        self.mappings.pop(type_, None)

    def update(self, mappings: Dict[str, Callable[[Optional[str]], Optional[Any]]]) -> None:
        self.mappings.update(mappings)

    @abstractmethod
    def convert(self, type_: str, value: Optional[str]) -> Optional[Any]:
        raise NotImplementedError  # pragma: no cover


class DefaultTypeConverter(Converter):
    def __init__(self) -> None:
        super().__init__(mappings=deepcopy(_DEFAULT_CONVERTERS), default=_to_default)

    def convert(self, type_: str, value: Optional[str]) -> Optional[Any]:
        converter = self.get(type_)
        return converter(value)

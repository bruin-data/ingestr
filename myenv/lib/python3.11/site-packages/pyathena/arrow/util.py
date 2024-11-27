# -*- coding: utf-8 -*-
from __future__ import annotations

from typing import TYPE_CHECKING, Any, Dict, Tuple, cast

if TYPE_CHECKING:
    from pyarrow import Schema
    from pyarrow.lib import DataType


def to_column_info(schema: "Schema") -> Tuple[Dict[str, Any], ...]:
    columns = []
    for field in schema:
        type_, precision, scale = get_athena_type(field.type)
        columns.append(
            {
                "Name": field.name,
                "Type": type_,
                "Precision": precision,
                "Scale": scale,
                "Nullable": "NULLABLE" if field.nullable else "NOT_NULL",
            }
        )
    return tuple(columns)


def get_athena_type(type_: "DataType") -> Tuple[str, int, int]:
    import pyarrow.lib as types

    if type_.id in [types.Type_BOOL]:  # 1
        return "boolean", 0, 0
    elif type_.id in [types.Type_UINT8, types.Type_INT8]:  # 2, 3
        return "tinyint", 3, 0
    elif type_.id in [types.Type_UINT16, types.Type_INT16]:  # 4, 5
        return "smallint", 5, 0
    elif type_.id in [types.Type_UINT32, types.Type_INT32]:  # 6, 7
        return "integer", 10, 0
    elif type_.id in [types.Type_UINT64, types.Type_INT64]:  # 8, 9
        return "bigint", 19, 0
    elif type_.id in [types.Type_HALF_FLOAT, types.Type_FLOAT]:  # 10, 11
        return "float", 17, 0
    elif type_.id in [types.Type_DOUBLE]:  # 12
        return "double", 17, 0
    elif type_.id in [types.Type_STRING, types.Type_LARGE_STRING]:  # 13, 34
        return "varchar", 2147483647, 0
    elif type_.id in [
        types.Type_BINARY,
        types.Type_FIXED_SIZE_BINARY,
        types.Type_LARGE_BINARY,
    ]:  # 14, 15, 35
        return "varbinary", 1073741824, 0
    elif type_.id in [types.Type_DATE32, types.Type_DATE64]:  # 16, 17
        return "date", 0, 0
    elif type_.id == types.Type_TIMESTAMP:  # 18
        return "timestamp", 3, 0
    elif type_.id in [types.Type_DECIMAL128, types.Decimal256Type]:  # 23, 24
        type_ = cast(types.Decimal128Type, type_)
        return "decimal", type_.precision, type_.scale
    elif type_.id in [
        types.Type_LIST,
        types.Type_FIXED_SIZE_LIST,
        types.Type_LARGE_LIST,
    ]:  # 25, 32, 36
        return "array", 0, 0
    elif type_.id in [types.Type_STRUCT]:  # 26
        return "row", 0, 0
    elif type_.id in [types.Type_MAP]:  # 30
        return "map", 0, 0
    else:
        return "string", 2147483647, 0

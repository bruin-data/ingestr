# -*- coding: utf-8 -*-
from __future__ import annotations

from typing import TYPE_CHECKING, Any, Dict, Tuple

if TYPE_CHECKING:
    from fastparquet.parquet_thrift import SchemaElement
    from fastparquet.schema import SchemaHelper


def to_column_info(schema: "SchemaHelper") -> Tuple[Dict[str, Any], ...]:
    from fastparquet.parquet_thrift import FieldRepetitionType

    columns = []
    for k, v in schema.schema_elements[0]["children"].items():
        type_, precision, scale = get_athena_type(v)
        if type_ == "row":
            # In the case of fastparquet, child elements of struct types are handled
            # as fields separated by dots.
            continue
        columns.append(
            {
                "Name": k,
                "Type": type_,
                "Precision": precision,
                "Scale": scale,
                "Nullable": "NOT_NULL"
                if v.repetition_type == FieldRepetitionType.REQUIRED
                else "NULLABLE",
            }
        )
    return tuple(columns)


def get_athena_type(type_: "SchemaElement") -> Tuple[str, int, int]:
    from fastparquet.parquet_thrift import ConvertedType, Type

    if type_.type in [Type.BOOLEAN]:
        return "boolean", 0, 0
    elif type_.type in [Type.INT32]:
        if type_.converted_type == ConvertedType.DATE:
            return "date", 0, 0
        else:
            return "integer", 10, 0
    elif type_.type in [Type.INT64]:
        return "bigint", 19, 0
    elif type_.type in [Type.INT96]:
        return "timestamp", 3, 0
    elif type_.type in [Type.FLOAT]:
        return "float", 17, 0
    elif type_.type in [Type.DOUBLE]:
        return "double", 17, 0
    elif type_.type in [Type.BYTE_ARRAY, Type.FIXED_LEN_BYTE_ARRAY]:
        if type_.converted_type == ConvertedType.UTF8:
            return "varchar", 2147483647, 0
        elif type_.converted_type == ConvertedType.DECIMAL:
            return "decimal", type_.precision, type_.scale
        else:
            return "varbinary", 1073741824, 0
    else:
        if type_.converted_type == ConvertedType.LIST:
            return "array", 0, 0
        elif type_.converted_type == ConvertedType.MAP:
            return "map", 0, 0
        else:
            children = getattr(type_, "children", [])
            if type_.type is None and type_.converted_type is None and children:
                return "row", 0, 0
            else:
                return "string", 2147483647, 0

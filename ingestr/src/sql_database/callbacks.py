from datetime import datetime

from sqlalchemy import text
from sqlalchemy import types as sa
from sqlalchemy.dialects import mysql


def type_adapter_callback(sql_type):
    if isinstance(sql_type, mysql.SET):
        return sa.JSON
    return sql_type


def chained_query_adapter_callback(query_adapters):
    """
    This function is used to chain multiple query adapters together,.
    This gives us the flexibility to introduce various adapters based on the given command parameters.
    """

    def callback(query, table):
        for adapter in query_adapters:
            query = adapter(query, table)

        return query

    return callback


def limit_callback(sql_limit: int, incremental_key: str):
    def callback(query, table):
        query = query.limit(sql_limit)
        if incremental_key:
            query = query.order_by(incremental_key)
        return query

    return callback


def custom_query_variable_subsitution(query_value: str, kwargs: dict):
    def callback(query, table, incremental=None, engine=None):
        params = {}
        if incremental:
            params["interval_start"] = (
                incremental.last_value
                if incremental.last_value is not None
                else datetime(year=1, month=1, day=1)
            )
            if incremental.end_value is not None:
                params["interval_end"] = incremental.end_value
        else:
            if ":interval_start" in query_value:
                params["interval_start"] = (
                    datetime.min
                    if kwargs.get("interval_start") is None
                    else kwargs.get("interval_start")
                )
            if ":interval_end" in query_value:
                params["interval_end"] = (
                    datetime.max
                    if kwargs.get("interval_end") is None
                    else kwargs.get("interval_end")
                )

        return text(query_value).bindparams(**params)

    return callback

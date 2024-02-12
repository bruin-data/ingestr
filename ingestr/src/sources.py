from typing import Callable

import dlt
import pendulum

from ingestr.src.sql_database import sql_table


class SqlSource:
    table_builder: Callable

    def __init__(self, table_builder=sql_table) -> None:
        self.table_builder = table_builder

    def dlt_source(self, uri: str, table: str, **kwargs):
        table_fields = table.split(".")
        if len(table_fields) != 2:
            raise ValueError("Table name must be in the format schema.<table>")

        incremental = None
        if kwargs.get("incremental_key"):
            incremental = dlt.sources.incremental(
                kwargs.get("incremental_key", ""),
                primary_key=(),
                initial_value=pendulum.now(),
            )

        table_instance = self.table_builder(
            credentials=uri,
            schema=table_fields[-2],
            table=table_fields[-1],
            incremental=incremental,
        )

        return table_instance

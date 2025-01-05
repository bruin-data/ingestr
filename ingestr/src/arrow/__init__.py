from typing import Any, Optional

import dlt
from dlt.common.schema.typing import TColumnNames, TTableSchemaColumns
from dlt.extract.items import TTableHintTemplate


def memory_mapped_arrow(
    path: str,
    columns: Optional[TTableSchemaColumns] = None,
    primary_key: Optional[TTableHintTemplate[TColumnNames]] = None,
    merge_key: Optional[TTableHintTemplate[TColumnNames]] = None,
    incremental: Optional[dlt.sources.incremental[Any]] = None,
):
    @dlt.resource(
        name="arrow_mmap",
        columns=columns,  # type: ignore
        primary_key=primary_key,  # type: ignore
        merge_key=merge_key,  # type: ignore
    )
    def arrow_mmap(
        incremental: Optional[dlt.sources.incremental[Any]] = incremental,
    ):
        import pyarrow as pa  # type: ignore
        import pyarrow.ipc as ipc  # type: ignore

        with pa.memory_map(path, "rb") as mmap:
            reader: ipc.RecordBatchFileReader = ipc.open_file(mmap)
            table = reader.read_all()

        last_value = None
        end_value = None
        if incremental:
            if incremental.cursor_path not in table.column_names:
                raise KeyError(
                    f"Cursor column '{incremental.cursor_path}' does not exist in table"
                )

            last_value = incremental.last_value
            end_value = incremental.end_value

        if last_value is not None:
            # Check if the column is a date type
            if pa.types.is_temporal(table.schema.field(incremental.cursor_path).type):  # type: ignore
                if not isinstance(last_value, pa.TimestampScalar):
                    last_value = pa.scalar(last_value, type=pa.timestamp("ns"))

                table = table.filter(
                    pa.compute.field(incremental.cursor_path) > last_value  # type: ignore
                )
            else:
                # For non-date types, use direct comparison
                table = table.filter(
                    pa.compute.field(incremental.cursor_path) > last_value  # type: ignore
                )

        if end_value is not None:
            if pa.types.is_timestamp(table.schema.field(incremental.cursor_path).type):  # type: ignore
                # Convert end_value to timestamp if it's not already
                if not isinstance(end_value, pa.TimestampScalar):
                    end_value = pa.scalar(end_value, type=pa.timestamp("ns"))
                table = table.filter(
                    pa.compute.field(incremental.cursor_path) < end_value  # type: ignore
                )
            else:
                # For non-date types, use direct comparison
                table = table.filter(
                    pa.compute.field(incremental.cursor_path) < end_value  # type: ignore
                )

        yield table

    return arrow_mmap

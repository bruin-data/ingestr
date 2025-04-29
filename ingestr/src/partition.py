from typing import Dict

from dlt.common.schema.typing import TColumnSchema
from dlt.sources import DltResource, DltSource

import ingestr.src.resource as resource


def apply_athena_hints(
    source: DltSource | DltResource,
    partition_column: str,
    additional_hints: Dict[str, TColumnSchema] = {},
) -> None:
    from dlt.destinations.adapters import athena_adapter, athena_partition

    def _apply_partition_hint(resource: DltResource) -> None:
        columns = resource.columns if resource.columns else {}

        partition_hint = (
            columns.get(partition_column)  # type: ignore
            or additional_hints.get(partition_column)
        )

        athena_adapter(
            resource,
            athena_partition.day(partition_column)
            if partition_hint
            and partition_hint.get("data_type") in ("timestamp", "date")
            else partition_column,
        )

    resource.for_each(source, _apply_partition_hint)

from dlt.destinations.adapters import athena_adapter, athena_partition
from dlt.sources import DltResource, DltSource

import ingestr.src.resource as resource


def apply_athena_hints(source: DltSource | DltResource, partition_column: str) -> None:
    def _apply_partition_hint(resource: DltResource) -> None:
        if resource.columns is None:
            return

        partition_hint = resource.columns.get(partition_column)  # type: ignore[union-attr]
        if partition_hint is None:
            return

        athena_adapter(
            resource,
            athena_partition.day(partition_column)
            if partition_hint["data_type"] in ("timestamp", "date")
            else partition_column,
        )

    resource.for_each(source, _apply_partition_hint)

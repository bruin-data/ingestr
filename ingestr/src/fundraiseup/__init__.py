"""Fundraiseup source for ingesting donations, events, fundraisers, recurring plans, and supporters."""

from typing import Any, Dict, Generator, Iterable, TypedDict

import dlt
import pendulum
from dlt.common.time import ensure_pendulum_datetime
from dlt.sources import DltResource

from .client import FundraiseupClient


class DonationCursor(TypedDict):
    id: str
    created_at: pendulum.DateTime


def order_by_created(record) -> DonationCursor:
    last_value = None
    if len(record) == 1:
        (record,) = record
    else:
        record, last_value = record

    cursor: DonationCursor = {
        "id": record["id"],
        "created_at": ensure_pendulum_datetime(record["created_at"]),
    }

    if last_value is None:
        return cursor

    return max(cursor, last_value, key=lambda v: v["created_at"])


@dlt.source(name="fundraiseup", max_table_nesting=0)
def fundraiseup_source(api_key: str) -> Iterable[DltResource]:
    """
    Return resources for Fundraiseup API.

    Args:
        api_key: API key for authentication

    Returns:
        Iterable of DLT resources
    """
    client = FundraiseupClient(api_key=api_key)

    # Define available resources and their configurations
    resources = {
        "donations": {"write_disposition": "replace", "primary_key": "id"},
        "events": {"write_disposition": "replace", "primary_key": "id"},
        "fundraisers": {"write_disposition": "replace", "primary_key": "id"},
        "recurring_plans": {"write_disposition": "replace", "primary_key": "id"},
        "supporters": {"write_disposition": "replace", "primary_key": "id"},
    }

    def create_resource(resource_name: str, config: Dict[str, Any]) -> DltResource:
        """Create a DLT resource dynamically."""

        @dlt.resource(
            name=resource_name,
            write_disposition=config["write_disposition"],
            primary_key=config["primary_key"],
        )
        def generic_resource() -> Generator[Dict[str, Any], None, None]:
            """Generic resource that yields batches directly."""
            for batch in client.get_paginated_data(resource_name):
                yield batch  # type: ignore[misc]

        return generic_resource()

    @dlt.resource(
        name="donations:incremental",
        write_disposition="merge",
        primary_key="id",
    )
    def donations_incremental(
        last_record: dlt.sources.incremental[DonationCursor] = dlt.sources.incremental(
            "$",
            range_start="closed",
            range_end="closed",
            last_value_func=order_by_created,
        ),
    ):
        params = {}
        if last_record.last_value is not None:
            params["starting_after"] = last_record.last_value["id"]
        for batch in client.get_paginated_data("donations", params=params):
            yield batch  # type: ignore[misc]

    # Return all resources
    return [donations_incremental] + [
        create_resource(name, config) for name, config in resources.items()
    ]

"""Fundraiseup source for ingesting donations, events, fundraisers, recurring plans, and supporters."""

from typing import Any, Dict, Generator, Iterable

import dlt
from dlt.sources import DltResource

from .client import FundraiseupClient


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

    # Return all resources
    return [create_resource(name, config) for name, config in resources.items()]

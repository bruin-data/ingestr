"""Fundraiseup source for ingesting donations, events, fundraisers, recurring plans, and supporters."""

from typing import Any, Dict, Generator, Iterable

import dlt
from dlt.sources import DltResource

from .client import FundraiseupClient

# Resource definitions
# resources ending in `-incremental` will be configured
# to load resources incrementally using primary_key
RESOURCES = {
    "donations": {"write_disposition": "replace", "primary_key": "id"},
    "donations-incremental": {"write_disposition": "merge", "primary_key": "id"},
    "events": {"write_disposition": "replace", "primary_key": "id"},
    "fundraisers": {"write_disposition": "replace", "primary_key": "id"},
    "recurring_plans": {"write_disposition": "replace", "primary_key": "id"},
    "supporters": {"write_disposition": "replace", "primary_key": "id"},
}

INCREMENTAL_SUFFIX="-incremental"

def read_remote_state(name):
    p = dlt.current.pipeline()
    if p.first_run:
        return {}

    remote = p._restore_state_from_destination()
    return remote["sources"]["fundraiseup"]["resources"][name]


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

    def create_resource(resource_name: str, config: Dict[str, Any]) -> DltResource:
        """Create a DLT resource dynamically."""

        incremental = resource_name.endswith(INCREMENTAL_SUFFIX)    
        @dlt.resource(
            name=resource_name,
            write_disposition=config["write_disposition"],
            primary_key=config["primary_key"],
        )
        def generic_resource() -> Generator[Dict[str, Any], None, None]:
            """Generic resource that yields batches directly."""
            name = resource_name if incremental is False else resource_name.removesuffix(INCREMENTAL_SUFFIX)
            state = dlt.current.resource_state()
            params = {}
            primary_key = config["primary_key"]
            if incremental:
                remote = read_remote_state(resource_name)
                last_id = state.setdefault(
                    primary_key,
                    remote.get(primary_key, None) 
                )
                if last_id:
                    params["starting_after"] = last_id

            for batch in client.get_paginated_data(name, params):
                yield batch  # type: ignore[misc]
                state[primary_key] = batch[-1].get("id")

        return generic_resource

    # Return all resources
    return [create_resource(name, config) for name, config in RESOURCES.items()]

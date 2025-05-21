from typing import Iterable, Iterator

import dlt
from dlt.sources import DltResource

from .helpers import AttioClient


@dlt.source(max_table_nesting=0)
def attio_source(
    api_key: str,
    params: list[str],
) -> Iterable[DltResource]:
    base_url = "https://api.attio.com/v2"
    attio_client = AttioClient(api_key)

    @dlt.resource(
        name="objects",
        primary_key=["workspace_id", "object_id"],
        write_disposition="merge",
        columns={
            "created_at": {"data_type": "timestamp", "partition": True},
        },
    )
    def fetch_objects() -> Iterator[dict]:
        if len(params) != 0:
            raise ValueError("Objects table must be in the format `objects`")

        url = f"{base_url}/objects"
        yield attio_client.fetch_attributes(url, "get")

    @dlt.resource(
        name="records",
        primary_key=["workspace_id", "object_id", "record_id"],
        write_disposition="merge",
        columns={
            "created_at": {"data_type": "timestamp", "partition": True},
        },
    )
    def fetch_records() -> Iterator[dict]:
        if len(params) != 1:
            raise ValueError(
                "Records table must be in the format `records:{object_api_slug}`"
            )

        object_id = params[0]
        url = f"{base_url}/objects/{object_id}/records/query"

        yield attio_client.fetch_attributes(url, "post")

    @dlt.resource(
        name="lists",
        primary_key=["workspace_id", "list_id"],
        write_disposition="merge",
        columns={
            "created_at": {"data_type": "timestamp", "partition": True},
        },
    )
    def fetch_lists() -> Iterator[dict]:
        url = f"{base_url}/lists"
        yield attio_client.fetch_attributes(url, "get")

    @dlt.resource(
        name="list_entries",
        primary_key=["workspace_id", "list_id", "entry_id"],
        write_disposition="merge",
        columns={
            "created_at": {"data_type": "timestamp", "partition": True},
        },
    )
    def fetch_list_entries() -> Iterator[dict]:
        if len(params) != 1:
            raise ValueError(
                "List entries table must be in the format `list_entries:{list_id}`"
            )
        url = f"{base_url}/lists/{params[0]}/entries/query"

        yield attio_client.fetch_attributes(url, "post")

    @dlt.resource(
        name="all_list_entries",
        primary_key=["workspace_id", "list_id", "entry_id"],
        write_disposition="merge",
        columns={
            "created_at": {"data_type": "timestamp", "partition": True},
        },
    )
    def fetch_all_list_entries() -> Iterator[dict]:
        if len(params) != 1:
            raise ValueError(
                "All list entries table must be in the format `all_list_entries:{object_api_slug}`"
            )
        yield attio_client.fetch_all_list_entries_for_object(params[0])

    return (
        fetch_objects,
        fetch_records,
        fetch_lists,
        fetch_list_entries,
        fetch_all_list_entries,
    )

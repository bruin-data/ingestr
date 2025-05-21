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
        write_disposition="replace",
        columns={
            "created_at": {"data_type": "timestamp", "partition": True},
        },
    )
    def fetch_objects() -> Iterator[dict]:
        if len(params) != 0:
            raise ValueError("Objects table must be in the format `objects`")

        url = f"{base_url}/objects"
        yield attio_client.fetch_data(url, "get")

    @dlt.resource(
        name="records",
        write_disposition="replace",
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

        yield attio_client.fetch_data(url, "post")

    @dlt.resource(
        name="lists",
        write_disposition="replace",
        columns={
            "created_at": {"data_type": "timestamp", "partition": True},
        },
    )
    def fetch_lists() -> Iterator[dict]:
        url = f"{base_url}/lists"
        yield attio_client.fetch_data(url, "get")

    @dlt.resource(
        name="list_entries",
        write_disposition="replace",
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

        yield attio_client.fetch_data(url, "post")

    @dlt.resource(
        name="all_list_entries",
        write_disposition="replace",
        columns={
            "created_at": {"data_type": "timestamp", "partition": True},
        },
    )
    def fetch_all_list_entries() -> Iterator[dict]:
        if len(params) != 1:
            raise ValueError(
                "All list entries table must be in the format `all_list_entries:{object_api_slug}`"
            )
        url = f"{base_url}/lists"
        for lst in attio_client.fetch_data(url, "get"):
            if params[0] in lst["parent_object"]:
                url = f"{base_url}/lists/{lst['id']['list_id']}/entries/query"
                yield from attio_client.fetch_data(url, "post")

    return (
        fetch_objects,
        fetch_records,
        fetch_lists,
        fetch_list_entries,
        fetch_all_list_entries,
    )

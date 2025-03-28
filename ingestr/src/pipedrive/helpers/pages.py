from itertools import chain
from typing import (
    Any,
    Dict,
    Iterable,
    Iterator,
    List,
    TypeVar,
    Union,
)

import dlt
from dlt.sources.helpers import requests

from ..typing import TDataPage
from .custom_fields_munger import rename_fields


def get_pages(
    entity: str, pipedrive_api_key: str, extra_params: Dict[str, Any] = None
) -> Iterator[List[Dict[str, Any]]]:
    """
    Generic method to retrieve endpoint data based on the required headers and params.

    Args:
        entity: the endpoint you want to call
        pipedrive_api_key:
        extra_params: any needed request params except pagination.

    Returns:

    """
    headers = {"Content-Type": "application/json"}
    params = {"api_token": pipedrive_api_key}
    if extra_params:
        params.update(extra_params)
    url = f"https://app.pipedrive.com/v1/{entity}"
    yield from _paginated_get(url, headers=headers, params=params)


def get_recent_items_incremental(
    entity: str,
    pipedrive_api_key: str,
    since_timestamp: dlt.sources.incremental[str] = dlt.sources.incremental(
        "update_time|modified", "1970-01-01 00:00:00"
    ),
) -> Iterator[TDataPage]:
    """Get a specific entity type from /recents with incremental state."""
    yield from _get_recent_pages(entity, pipedrive_api_key, since_timestamp.last_value)


def _paginated_get(
    url: str, headers: Dict[str, Any], params: Dict[str, Any]
) -> Iterator[List[Dict[str, Any]]]:
    """
    Requests and yields data 500 records at a time
    Documentation: https://pipedrive.readme.io/docs/core-api-concepts-pagination
    """
    # pagination start and page limit
    params["start"] = 0
    params["limit"] = 500
    while True:
        page = requests.get(url, headers=headers, params=params).json()
        # yield data only
        data = page["data"]
        if data:
            yield data
        # check if next page exists
        pagination_info = page.get("additional_data", {}).get("pagination", {})
        # is_next_page is set to True or False
        if not pagination_info.get("more_items_in_collection", False):
            break
        params["start"] = pagination_info.get("next_start")


T = TypeVar("T")


def _extract_recents_data(data: Iterable[Dict[str, Any]]) -> List[Dict[str, Any]]:
    """Results from recents endpoint contain `data` key which is either a single entity or list of entities

    This returns a flat list of entities from an iterable of recent results
    """
    return [
        data_item
        for data_item in chain.from_iterable(
            (_list_wrapped(item["data"]) for item in data)
        )
        if data_item is not None
    ]


def _list_wrapped(item: Union[List[T], T]) -> List[T]:
    if isinstance(item, list):
        return item
    return [item]


def _get_recent_pages(
    entity: str, pipedrive_api_key: str, since_timestamp: str
) -> Iterator[TDataPage]:
    custom_fields_mapping = (
        dlt.current.source_state().get("custom_fields_mapping", {}).get(entity, {})
    )
    pages = get_pages(
        "recents",
        pipedrive_api_key,
        extra_params=dict(since_timestamp=since_timestamp, items=entity),
    )
    pages = (_extract_recents_data(page) for page in pages)
    for page in pages:
        yield rename_fields(page, custom_fields_mapping)


__source_name__ = "pipedrive"

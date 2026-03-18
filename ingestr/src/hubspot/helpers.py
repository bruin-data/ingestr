# Copyright 2022-2025 ScaleVector
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#   http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""Hubspot source helpers"""

import urllib.parse
from typing import Any, Dict, Iterator, List, Optional

import pendulum
from dlt.sources.helpers import requests

from .settings import (
    DEFAULT_LAST_MODIFIED_PROPERTY,
    LAST_MODIFIED_PROPERTY,
    OBJECT_TYPE_PLURAL,
)

BASE_URL = "https://api.hubapi.com/"


def get_url(endpoint: str) -> str:
    """Get absolute hubspot endpoint URL"""
    return urllib.parse.urljoin(BASE_URL, endpoint)


def _get_headers(api_key: str) -> Dict[str, str]:
    """
    Return a dictionary of HTTP headers to use for API requests, including the specified API key.

    Args:
        api_key (str): The API key to use for authentication, as a string.

    Returns:
        dict: A dictionary of HTTP headers to include in API requests, with the `Authorization` header
            set to the specified API key in the format `Bearer {api_key}`.

    """
    # Construct the dictionary of HTTP headers to use for API requests
    return dict(authorization=f"Bearer {api_key}")


def extract_property_history(objects: List[Dict[str, Any]]) -> Iterator[Dict[str, Any]]:
    for item in objects:
        history = item.get("propertiesWithHistory")
        if not history:
            continue
        # Yield a flat list of property history entries
        for key, changes in history.items():
            if not changes:
                continue
            for entry in changes:
                yield {"object_id": item["id"], "property_name": key, **entry}


def fetch_property_history(
    endpoint: str,
    api_key: str,
    props: str,
    params: Optional[Dict[str, Any]] = None,
) -> Iterator[List[Dict[str, Any]]]:
    """Fetch property history from the given CRM endpoint.

    Args:
        endpoint: The endpoint to fetch data from, as a string.
        api_key: The API key to use for authentication, as a string.
        props: A comma separated list of properties to retrieve the history for
        params: Optional dict of query params to include in the request

    Yields:
         List of property history entries (dicts)
    """
    # Construct the URL and headers for the API request
    url = get_url(endpoint)
    headers = _get_headers(api_key)

    params = dict(params or {})
    params["propertiesWithHistory"] = props
    params["limit"] = 50
    # Make the API request
    r = requests.get(url, headers=headers, params=params)
    # Parse the API response and yield the properties of each result

    # Parse the response JSON data
    _data = r.json()
    while _data is not None:
        if "results" in _data:
            yield list(extract_property_history(_data["results"]))

        # Follow pagination links if they exist
        _next = _data.get("paging", {}).get("next", None)
        if _next:
            next_url = _next["link"]
            # Get the next page response
            r = requests.get(next_url, headers=headers)
            _data = r.json()
        else:
            _data = None


def fetch_data(
    endpoint: str,
    api_key: str,
    params: Optional[Dict[str, Any]] = None,
    resource_name: str = None,
) -> Iterator[List[Dict[str, Any]]]:
    """
    Fetch data from HUBSPOT endpoint using a specified API key and yield the properties of each result.
    For paginated endpoint this function yields item from all pages.

    Args:
        endpoint (str): The endpoint to fetch data from, as a string.
        api_key (str): The API key to use for authentication, as a string.
        params: Optional dict of query params to include in the request

    Yields:
        A List of CRM object dicts

    Raises:
        requests.exceptions.HTTPError: If the API returns an HTTP error status code.

    Notes:
        This function uses the `requests` library to make a GET request to the specified endpoint, with
        the API key included in the headers. If the API returns a non-successful HTTP status code (e.g.
        404 Not Found), a `requests.exceptions.HTTPError` exception will be raised.

        The `endpoint` argument should be a relative URL, which will be appended to the base URL for the
        API. The `params` argument is used to pass additional query parameters to the request

        This function also includes a retry decorator that will automatically retry the API call up to
        3 times with a 5-second delay between retries, using an exponential backoff strategy.
    """
    # Construct the URL and headers for the API request
    url = get_url(endpoint)
    headers = _get_headers(api_key)

    # Make the API request
    r = requests.get(url, headers=headers, params=params)
    # Parse the API response and yield the properties of each result
    # Parse the response JSON data
    _data = r.json()

    # Yield the properties of each result in the API response
    while _data is not None:
        if "results" in _data:
            _objects: List[Dict[str, Any]] = []
            for _result in _data["results"]:
                if resource_name == "schemas":
                    _objects.append(
                        {
                            "name": _result["labels"].get("singular", ""),
                            "objectTypeId": _result.get("objectTypeId", ""),
                            "id": _result.get("id", ""),
                            "fullyQualifiedName": _result.get("fullyQualifiedName", ""),
                            "properties": _result.get("properties", ""),
                            "createdAt": _result.get("createdAt", ""),
                            "updatedAt": _result.get("updatedAt", ""),
                        }
                    )
                elif resource_name == "owners":
                    _objects.append(
                        {
                            "id": _result.get("id", ""),
                            "email": _result.get("email", ""),
                            "type": _result.get("type", ""),
                            "firstName": _result.get("firstName", ""),
                            "lastName": _result.get("lastName", ""),
                            "createdAt": _result.get("createdAt", ""),
                            "updatedAt": _result.get("updatedAt", ""),
                            "userId": _result.get("userId", ""),
                            "teams": _result.get("teams", []),
                        }
                    )
                else:
                    _obj = _result.get("properties", _result)
                    if "id" not in _obj and "id" in _result:
                        # Move id from properties to top level
                        _obj["id"] = _result["id"]

                    if "associations" in _result:
                        for association in _result["associations"]:
                            __values = [
                                {
                                    "value": _obj["hs_object_id"],
                                    f"{association}_id": __r["id"],
                                }
                                for __r in _result["associations"][association][
                                    "results"
                                ]
                            ]

                            # remove duplicates from list of dicts
                            __values = [
                                dict(t) for t in {tuple(d.items()) for d in __values}
                            ]

                            _obj[association] = __values

                    _objects.append(_obj)
            yield _objects

        # Follow pagination links if they exist
        _next = _data.get("paging", {}).get("next", None)
        if _next:
            next_url = _next["link"]
            # Get the next page response
            r = requests.get(next_url, headers=headers)
            _data = r.json()
        else:
            _data = None


def _get_property_names(api_key: str, object_type: str) -> List[str]:
    """
    Retrieve property names for a given entity from the HubSpot API.

    Args:
        entity: The entity name for which to retrieve property names.

    Returns:
        A list of property names.

    Raises:
        Exception: If an error occurs during the API request.
    """
    properties = []
    endpoint = f"/crm/v3/properties/{OBJECT_TYPE_PLURAL[object_type]}"

    for page in fetch_data(endpoint, api_key):
        properties.extend([prop["name"] for prop in page])

    return properties


def _fetch_associations_batch(
    from_object_type: str,
    to_object_type: str,
    object_ids: List[str],
    api_key: str,
) -> Dict[str, List[str]]:
    """Fetch associations for a batch of objects via the HubSpot v4 batch associations API.

    Returns a dict mapping from_id -> list of to_ids.
    Returns an empty dict if the association type is unsupported.
    """
    if not object_ids:
        return {}

    url = get_url(
        f"/crm/v4/associations/{from_object_type}/{to_object_type}/batch/read"
    )
    headers = _get_headers(api_key)
    r = requests.post(
        url, headers=headers, json={"inputs": [{"id": oid} for oid in object_ids]}
    )

    if r.status_code in (400, 404):
        return {}
    r.raise_for_status()

    result: Dict[str, List[str]] = {}
    for item in r.json().get("results", []):
        from_id = str(item.get("from", {}).get("id", ""))
        to_ids = [
            str(a["toObjectId"]) for a in item.get("to", []) if a.get("toObjectId")
        ]
        if from_id and to_ids:
            result[from_id] = to_ids
    return result


def fetch_data_search(
    object_type: str,
    api_key: str,
    properties: str,
    start_date_ms: str,
    end_date_ms: Optional[str] = None,
    association_types: Optional[List[str]] = None,
) -> Iterator[List[Dict[str, Any]]]:
    import logging

    logger = logging.getLogger("hubspot.search")

    url = get_url(f"/crm/v3/objects/{OBJECT_TYPE_PLURAL[object_type]}/search")
    headers = _get_headers(api_key)
    from_type = OBJECT_TYPE_PLURAL[object_type]
    modified_prop = LAST_MODIFIED_PROPERTY.get(
        object_type, DEFAULT_LAST_MODIFIED_PROPERTY
    )

    props_list = [p for p in properties.split(",") if p]
    current_start_ms = start_date_ms
    window_num = 0

    while True:
        window_num += 1
        filters = [
            {
                "propertyName": modified_prop,
                "operator": "GTE",
                "value": current_start_ms,
            }
        ]
        if end_date_ms is not None:
            filters.append(
                {
                    "propertyName": modified_prop,
                    "operator": "LTE",
                    "value": end_date_ms,
                }
            )

        start_human = pendulum.from_timestamp(int(current_start_ms) / 1000).to_iso8601_string()
        end_human = pendulum.from_timestamp(int(end_date_ms) / 1000).to_iso8601_string() if end_date_ms else "none"
        logger.info(
            f"[hubspot] search window #{window_num} for {object_type}: "
            f"GTE={current_start_ms} ({start_human}) LTE={end_date_ms} ({end_human})"
        )

        body: Dict[str, Any] = {
            "filterGroups": [{"filters": filters}],
            "properties": props_list,
            "sorts": [{"propertyName": modified_prop, "direction": "ASCENDING"}],
            "limit": 100,
        }

        total_yielded = 0
        max_modified_ms: Optional[str] = None
        page_num = 0

        while True:
            page_num += 1
            r = requests.post(url, headers=headers, json=body)
            r.raise_for_status()
            _data = r.json()

            if _data.get("status") == "error":
                raise ValueError(
                    f"HubSpot search error: {_data.get('message')} (correlationId: {_data.get('correlationId')})"
                )

            result_count = len(_data.get("results", []))
            api_total = _data.get("total", "?")
            logger.info(
                f"[hubspot] window #{window_num} page #{page_num}: "
                f"got {result_count} results, api_total={api_total}, yielded_so_far={total_yielded}"
            )

            if "results" in _data:
                _objects: List[Dict[str, Any]] = []
                for _result in _data["results"]:
                    _obj = _result.get("properties", _result)
                    if "id" not in _obj and "id" in _result:
                        _obj["id"] = _result["id"]
                    _objects.append(_obj)

                    # Track the max modified date we've seen
                    mod_val = _obj.get(modified_prop)
                    if mod_val is not None:
                        mod_ms = str(
                            int(
                                pendulum.parse(mod_val).timestamp() * 1000  # type: ignore[union-attr]
                            )
                        )
                        if max_modified_ms is None or int(mod_ms) > int(max_modified_ms):
                            max_modified_ms = mod_ms

                if association_types and _objects:
                    obj_ids = [
                        str(obj.get("hs_object_id") or obj.get("id") or "")
                        for obj in _objects
                    ]
                    for assoc_type in association_types:
                        if not assoc_type:
                            continue
                        assoc_map = _fetch_associations_batch(
                            from_type, assoc_type, obj_ids, api_key
                        )
                        for obj in _objects:
                            obj_id = str(obj.get("hs_object_id") or obj.get("id") or "")
                            values = [
                                {"value": obj_id, f"{assoc_type}_id": aid}
                                for aid in assoc_map.get(obj_id, [])
                            ]
                            obj[assoc_type] = [
                                dict(t) for t in {tuple(d.items()) for d in values}
                            ]

                total_yielded += len(_objects)
                yield _objects

            # Break BEFORE trying to fetch beyond the 10k limit — HubSpot's
            # search API hangs/errors when paging past 10,000 results.
            if total_yielded >= 10000:
                logger.info(
                    f"[hubspot] window #{window_num}: reached {total_yielded} rows, "
                    f"breaking inner loop to restart with new GTE"
                )
                break

            _next = _data.get("paging", {}).get("next", None)
            if _next:
                body["after"] = _next["after"]
            else:
                break

        max_human = pendulum.from_timestamp(int(max_modified_ms) / 1000).to_iso8601_string() if max_modified_ms else "none"
        logger.info(
            f"[hubspot] window #{window_num} done: total_yielded={total_yielded}, "
            f"max_modified={max_modified_ms} ({max_human})"
        )

        # HubSpot search API has a 10,000 result hard limit. If we hit it,
        # restart the search using the max modified date + 1ms as the new GTE
        # to avoid re-fetching the same rows.
        if total_yielded >= 10000 and max_modified_ms is not None:
            new_start = str(int(max_modified_ms) + 1)
            logger.info(
                f"[hubspot] hit 10k limit, restarting with new GTE={new_start} "
                f"({pendulum.from_timestamp(int(new_start) / 1000).to_iso8601_string()})"
            )
            current_start_ms = new_start
        else:
            break


def fetch_data_raw(
    endpoint: str, api_key: str, params: Optional[Dict[str, Any]] = None
) -> Iterator[List[Dict[str, Any]]]:
    url = get_url(endpoint)
    headers = _get_headers(api_key)
    r = requests.get(url, headers=headers, params=params)
    return r.json()

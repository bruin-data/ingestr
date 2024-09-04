from typing import Iterable

import dlt
import requests
from dlt.common.time import ensure_pendulum_datetime
from dlt.common.typing import TAnyDateTime, TDataItem
from dlt.sources import DltResource
from dlt.sources.helpers.requests import Client


def fetch_data(endpoint, datetime, api_key):
    base_url = "https://a.klaviyo.com/api/"
    headers = {
        "Authorization": f"Klaviyo-API-Key {api_key}",
        "accept": "application/json",
        "revision": "2024-07-15",
    }
    sort_filter = f"/?sort=-datetime&filter=greater-or-equal(datetime,{datetime})"
    url = base_url + endpoint + sort_filter

    def retry_on_limit(response: requests.Response, exception: BaseException) -> bool:
        return response.status_code == 429

    request_client = Client(
        request_timeout=8.0,
        raise_for_status=False,
        retry_condition=retry_on_limit,
        request_max_attempts=12,
        request_backoff_factor=2,
    ).session

    while True:
        response = request_client.get(url=url, headers=headers)
        result = response.json()
        events = result.get("data", [])

        for event in events:
            for attribute_key in event["attributes"]:
                event[attribute_key] = event["attributes"][attribute_key]
            del event["attributes"]
        yield events

        url = result["links"]["next"]
        if url is None:
            break


@dlt.source(max_table_nesting=0)
def klaviyo_source(api_key: str, start_date: TAnyDateTime) -> Iterable[DltResource]:
    start_date_obj = ensure_pendulum_datetime(start_date)

    @dlt.resource()
    def events(
        datetime=dlt.sources.incremental("datetime", start_date_obj.isoformat()),
    ) -> Iterable[TDataItem]:
        yield from fetch_data(
            endpoint="events", datetime=datetime.start_value, api_key=api_key
        )

    return events

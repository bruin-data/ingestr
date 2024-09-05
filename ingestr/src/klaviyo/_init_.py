from typing import Iterable, List, Tuple

import dlt
import pendulum
import requests
from dlt.common.time import ensure_pendulum_datetime
from dlt.common.typing import TAnyDateTime, TDataItem
from dlt.sources import DltResource
from dlt.sources.helpers.requests import Client


def split_date_range(
    start_date: pendulum.DateTime, end_date: pendulum.DateTime
) -> List[Tuple[pendulum.DateTime, pendulum.DateTime]]:
    interval = "days"
    if (end_date - start_date).days <= 1:
        interval = "hours"

    intervals = []
    current = start_date
    while current < end_date:
        next_date = min(current.add(**{interval: 1}), end_date)
        intervals.append((current.isoformat(), next_date.isoformat()))
        current = next_date
    return intervals


def fetch_data(
    session: requests.Session,
    api_key: str,
    endpoint: str,
    start_date: str,
    end_date: str,
):
    base_url = "https://a.klaviyo.com/api/"
    headers = {
        "Authorization": f"Klaviyo-API-Key {api_key}",
        "accept": "application/json",
        "revision": "2024-07-15",
    }
    sort_filter = f"/?sort=-datetime&filter=and(greater-or-equal(datetime,{start_date}),less-than(datetime,{end_date}))"
    url = base_url + endpoint + sort_filter

    all_events = []
    while True:
        response = session.get(url=url, headers=headers)
        result = response.json()
        events = result.get("data", [])

        for event in events:
            for attribute_key in event["attributes"]:
                event[attribute_key] = event["attributes"][attribute_key]
            del event["attributes"]

        all_events.extend(events)

        url = result["links"]["next"]
        if url is None:
            break

    return all_events


@dlt.source(max_table_nesting=0)
def klaviyo_source(api_key: str, start_date: TAnyDateTime) -> Iterable[DltResource]:
    start_date_obj = ensure_pendulum_datetime(start_date)

    @dlt.resource(write_disposition="append", primary_key="id", parallelized=True)
    def events(
        datetime=dlt.sources.incremental("datetime", start_date_obj.isoformat()),
    ) -> Iterable[TDataItem]:
        intervals = split_date_range(
            pendulum.parse(datetime.start_value), pendulum.now()
        )

        def retry_on_limit(
            response: requests.Response, exception: BaseException
        ) -> bool:
            return response.status_code == 429

        def create_client():
            return Client(
                request_timeout=8.0,
                raise_for_status=False,
                retry_condition=retry_on_limit,
                request_max_attempts=12,
                request_backoff_factor=2,
            ).session

        for start, end in intervals:

            def fetch_data_wrapper():
                return fetch_data(create_client(), api_key, "events", start, end)

            yield fetch_data_wrapper

    return events

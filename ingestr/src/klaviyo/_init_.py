from typing import Iterable

import dlt
import pendulum
import requests
from dlt.common.time import ensure_pendulum_datetime
from dlt.common.typing import TAnyDateTime, TDataItem
from dlt.sources import DltResource
from dlt.sources.helpers.requests import Client

from ingestr.src.klaviyo.client import KlaviyoClient
from ingestr.src.klaviyo.helpers import split_date_range


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

        client = KlaviyoClient(api_key)
        for start, end in intervals:
            yield lambda s=start, e=end: client.fetch_events(create_client(), s, e)

    return events

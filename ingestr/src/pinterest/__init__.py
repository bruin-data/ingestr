from typing import Iterable

import dlt
import pendulum
from dlt.common.time import ensure_pendulum_datetime
from dlt.common.typing import TDataItem
from dlt.sources import DltResource
from dlt.sources.helpers import requests


@dlt.source(name="pinterest", max_table_nesting=0)
def pinterest_source(
    start_date: pendulum.DateTime,
    access_token: str,
    page_size: int = 200,
    end_date: pendulum.DateTime | None = None,
) -> Iterable[DltResource]:
    session = requests.Session()
    session.headers.update({"Authorization": f"Bearer {access_token}"})
    base_url = "https://api.pinterest.com/v5"

    def fetch_data(
        endpoint: str,
        start_dt: pendulum.DateTime,
        end_dt: pendulum.DateTime,
    ) -> Iterable[TDataItem]:
        url = f"{base_url}/{endpoint}"
        params = {"page_size": page_size}
        bookmark = None
        while True:
            if bookmark:
                params["bookmark"] = bookmark

            resp = session.get(url, params=params)
            resp.raise_for_status()
            data = resp.json()
            items = data.get("items") or []

            for item in items:
                item_created = ensure_pendulum_datetime(item["created_at"])
                if item_created <= start_dt:
                    continue
                if item_created > end_dt:
                    continue
                item["created_at"] = item_created
                yield item

            bookmark = data.get("bookmark")
            if not bookmark:
                break

    @dlt.resource(write_disposition="merge", primary_key="id")
    def pins(
        datetime=dlt.sources.incremental(
            "created_at",
            initial_value=start_date,
            end_value=end_date,
        ),
    ) -> Iterable[TDataItem]:
        _start_date = datetime.last_value or start_date
        if end_date is None:
            _end_date = pendulum.now("UTC")
        else:
            _end_date = datetime.end_value
        yield from fetch_data("pins", _start_date, _end_date)

    @dlt.resource(write_disposition="merge", primary_key="id")
    def boards(
        datetime=dlt.sources.incremental(
            "created_at",
            initial_value=start_date,
            end_value=end_date,
        ),
    ) -> Iterable[TDataItem]:
        _start_date = datetime.last_value or start_date
        if end_date is None:
            _end_date = pendulum.now("UTC")
        else:
            _end_date = datetime.end_value
        yield from fetch_data("boards", _start_date, _end_date)

    return pins, boards

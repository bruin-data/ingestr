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

    @dlt.resource(write_disposition="merge", primary_key="id")
    def pins(
        datetime=dlt.sources.incremental(
            "created_at",
            initial_value=start_date,
            end_value=end_date,
        ),
    ) -> Iterable[TDataItem]:
        url = f"{base_url}/pins"
        params = {"page_size": page_size}
        bookmark = None

        _start_date = datetime.last_value or start_date
        _end_date = datetime.end_value or pendulum.now("UTC")
        while True:
            if bookmark:
                params["bookmark"] = bookmark

            resp = session.get(url, params=params)
            resp.raise_for_status()
            data = resp.json()
            items = data.get("items") or []

            for item in items:
                item_created = ensure_pendulum_datetime(item["created_at"])
                if item_created <= _start_date:
                    continue

                if item_created > _end_date:
                    continue

                yield item

            bookmark = data.get("bookmark")
            if not bookmark:
                break

    @dlt.resource(write_disposition="merge", primary_key="id")
    def boards(
        datetime=dlt.sources.incremental(
            "created_at",
            initial_value=start_date,
            end_value=end_date,
        ),
    ) -> Iterable[TDataItem]:
        url = f"{base_url}/boards"
        params = {"page_size": page_size}
        bookmark = None

        _start_date = datetime.last_value or start_date
        _end_date = datetime.end_value or pendulum.now("UTC")

        while True:
            if bookmark:
                params["bookmark"] = bookmark

            resp = session.get(url, params=params)
            resp.raise_for_status()
            data = resp.json()
            items = data.get("items") or []

            for item in items:
                item_created = ensure_pendulum_datetime(item["created_at"])

                if item_created <= _start_date:
                    continue

                if item_created > _end_date:
                    continue

                yield item

            bookmark = data.get("bookmark")
            if not bookmark:
                break

    return pins, boards

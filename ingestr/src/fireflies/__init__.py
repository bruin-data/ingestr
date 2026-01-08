from typing import Iterable, List, Optional

import dlt
import pendulum
from dlt.common.time import ensure_pendulum_datetime
from dlt.common.typing import TDataItem
from dlt.sources import DltResource

from .helpers import FirefliesAPI


@dlt.source(name="fireflies", max_table_nesting=0)
def fireflies_source(
    api_key: str,
    start_datetime: Optional[pendulum.DateTime],
    end_datetime: Optional[pendulum.DateTime],
    granularity: Optional[str] = None,
) -> List[DltResource]:
    fireflies_api = FirefliesAPI(api_key=api_key)

    start_datetime = (
        ensure_pendulum_datetime(start_datetime) if start_datetime else None
    )
    end_datetime = ensure_pendulum_datetime(end_datetime) if end_datetime else None

    # Select fetch method based on granularity
    def get_analytics_fetcher():
        if granularity == "DAY":
            return fireflies_api.fetch_analytics_daily
        elif granularity == "HOUR":
            return fireflies_api.fetch_analytics_hourly
        elif granularity == "MONTH":
            return fireflies_api.fetch_analytics_monthly
        else:
            return fireflies_api.fetch_analytics

    @dlt.resource(write_disposition="replace")
    def active_meetings() -> Iterable[TDataItem]:
        for page in fireflies_api.fetch_active_meetings():
            for item in page:
                yield item

    @dlt.resource(
        write_disposition="merge",
        primary_key=["start_time", "end_time"],
    )
    def analytics(
        updated_at: dlt.sources.incremental[
            pendulum.DateTime
        ] = dlt.sources.incremental(
            "end_time",
            initial_value=start_datetime
            if start_datetime
            else pendulum.datetime(1970, 1, 1, tz="UTC"),
            end_value=end_datetime if end_datetime else None,
            range_end="closed" if end_datetime else "open",
            range_start="closed",
        ),
    ) -> Iterable[TDataItem]:
        from_date_dt = updated_at.last_value
        to_date_dt = (
            updated_at.end_value if updated_at.end_value else pendulum.now(tz="UTC")
        )

        from_date_iso = from_date_dt.to_iso8601_string() if from_date_dt else None
        to_date_iso = to_date_dt.to_iso8601_string() if to_date_dt else None

        fetch_method = get_analytics_fetcher()
        for page in fetch_method(
            from_date=from_date_iso,
            to_date=to_date_iso,
        ):
            for item in page:
                if "end_time" in item and isinstance(item["end_time"], str):
                    item["end_time"] = pendulum.parse(item["end_time"])
                yield item

    @dlt.resource(write_disposition="replace")
    def channels() -> Iterable[TDataItem]:
        for page in fireflies_api.fetch_channels():
            for item in page:
                yield item

    @dlt.resource(write_disposition="replace")
    def users() -> Iterable[TDataItem]:
        for page in fireflies_api.fetch_users():
            for item in page:
                yield item

    @dlt.resource(write_disposition="replace")
    def user_groups() -> Iterable[TDataItem]:
        for page in fireflies_api.fetch_user_groups():
            for item in page:
                yield item

    @dlt.resource(
        write_disposition="merge",
        primary_key="id",
    )
    def transcripts(
        updated_at: dlt.sources.incremental[
            pendulum.DateTime
        ] = dlt.sources.incremental(
            "date",
            initial_value=start_datetime
            if start_datetime
            else pendulum.datetime(1970, 1, 1, tz="UTC"),
            end_value=end_datetime if end_datetime else None,
            range_end="closed" if end_datetime else "open",
            range_start="closed",
        ),
    ) -> Iterable[TDataItem]:
        from_date_dt = updated_at.last_value
        to_date_dt = (
            updated_at.end_value if updated_at.end_value else pendulum.now(tz="UTC")
        )

        from_date_iso = from_date_dt.to_iso8601_string() if from_date_dt else None
        to_date_iso = to_date_dt.to_iso8601_string() if to_date_dt else None

        for page in fireflies_api.fetch_transcripts(
            from_date=from_date_iso,
            to_date=to_date_iso,
        ):
            for item in page:
                if "date" in item and isinstance(item["date"], (int, float)):
                    item["date"] = pendulum.from_timestamp(item["date"] / 1000)
                yield item

    @dlt.resource(write_disposition="replace")
    def bites() -> Iterable[TDataItem]:
        for page in fireflies_api.fetch_bites():
            for item in page:
                yield item

    @dlt.resource(write_disposition="replace")
    def contacts() -> Iterable[TDataItem]:
        for page in fireflies_api.fetch_contacts():
            for item in page:
                yield item

    return [
        active_meetings,
        analytics,
        channels,
        users,
        transcripts,
        user_groups,
        bites,
        contacts,
    ]

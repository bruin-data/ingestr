from typing import Iterable, List, Literal, Optional

import dlt
import pendulum
from dlt.common.typing import TDataItem
from dlt.sources import DltResource

from .helpers import FirefliesAPI


@dlt.source(name="fireflies", max_table_nesting=0)
def fireflies_source(
    api_key: str,
    start_datetime: Optional[pendulum.DateTime],
    end_datetime: Optional[pendulum.DateTime],
    channel_id: Optional[str] = None,
    user_id: Optional[str] = None,
    transcript_id: Optional[str] = None,
    bite_id: Optional[str] = None,
) -> List[DltResource]:
    fireflies_api = FirefliesAPI(api_key=api_key)

    # Convert DateTime to strings immediately to avoid DateTime objects in closure
    start_time_iso = start_datetime.to_iso8601_string() if start_datetime else None
    end_time_iso = end_datetime.to_iso8601_string() if end_datetime else None

    @dlt.resource(
        write_disposition="replace",
        primary_key="id",
    )
    def active_meetings() -> Iterable[TDataItem]:
        for page in fireflies_api.fetch_active_meetings():
            for item in page:
                yield item

    @dlt.resource(
        write_disposition="replace",
    )
    def analytics() -> Iterable[TDataItem]:
        if start_time_iso is None or end_time_iso is None:
            raise ValueError(
                "start_datetime and end_datetime are required for analytics"
            )

        for page in fireflies_api.fetch_analytics(start_time_iso, end_time_iso):
            for item in page:
                yield item

    @dlt.resource(
        write_disposition="replace",
        primary_key="id",
    )
    def channels() -> Iterable[TDataItem]:
        for page in fireflies_api.fetch_channels():
            for item in page:
                yield item

    @dlt.resource(
        write_disposition="replace",
        primary_key="id",
    )
    def channel() -> Iterable[TDataItem]:
        if channel_id is None:
            return
        for page in fireflies_api.fetch_channel(channel_id):
            for item in page:
                yield item

    @dlt.resource(
        write_disposition="replace",
        primary_key="user_id",
    )
    def users() -> Iterable[TDataItem]:
        for page in fireflies_api.fetch_users():
            for item in page:
                yield item

    @dlt.resource(
        write_disposition="replace",
        primary_key="user_id",
    )
    def user() -> Iterable[TDataItem]:
        if user_id is None:
            return
        for page in fireflies_api.fetch_user(user_id):
            for item in page:
                yield item

    @dlt.resource(
        write_disposition="replace",
        primary_key="id",
    )
    def transcript() -> Iterable[TDataItem]:
        if transcript_id is None:
            return
        for page in fireflies_api.fetch_transcript(transcript_id):
            for item in page:
                yield item

    @dlt.resource(
        write_disposition="replace",
        primary_key="id",
    )
    def user_groups() -> Iterable[TDataItem]:
        for page in fireflies_api.fetch_user_groups():
            for item in page:
                yield item

    @dlt.resource(
        write_disposition="replace",
        primary_key="id",
    )
    def transcripts() -> Iterable[TDataItem]:
        """Fetch all transcripts from Fireflies API."""
        for page in fireflies_api.fetch_transcripts():
            for item in page:
                yield item

    @dlt.resource(
        write_disposition="replace",
        primary_key="id",
    )
    def bite() -> Iterable[TDataItem]:
        if bite_id is None:
            return
        for page in fireflies_api.fetch_bite(bite_id):
            for item in page:
                yield item

    @dlt.resource(
        write_disposition="replace",
        primary_key="id",
    )
    def bites() -> Iterable[TDataItem]:
        """Fetch all bites from Fireflies API."""
        for page in fireflies_api.fetch_bites():
            for item in page:
                yield item

    @dlt.resource(
        write_disposition="replace",
        primary_key="email",
    )
    def contacts() -> Iterable[TDataItem]:
        for page in fireflies_api.fetch_contacts():
            for item in page:
                yield item

    return [
        active_meetings,
        analytics,
        channels,
        channel,
        users,
        user,
        transcript,
        transcripts,
        user_groups,
        bite,
        bites,
        contacts,
    ]

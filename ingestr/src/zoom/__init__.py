from typing import Any, Dict, Iterable, Sequence

import dlt
import pendulum
from dlt.common.typing import TAnyDateTime, TDataItem
from dlt.sources import DltResource

from .helpers import ZoomClient


@dlt.source(name="zoom", max_table_nesting=0)
def zoom_source(
    client_id: str,
    client_secret: str,
    account_id: str,
    start_date: pendulum.DateTime,
    end_date: pendulum.DateTime | None = None,
) -> Sequence[DltResource]:
    """Create a Zoom source with meetings resource for all users in the account."""
    client = ZoomClient(
        client_id=client_id,
        client_secret=client_secret,
        account_id=account_id,
    )

    @dlt.resource(write_disposition="merge", primary_key="id")
    def meetings(
        datetime: dlt.sources.incremental[TAnyDateTime] = dlt.sources.incremental(
            "start_time",
            initial_value=start_date.isoformat(),
            end_value=end_date.isoformat() if end_date is not None else None,
            range_start="closed",
            range_end="closed",
        ),
    ) -> Iterable[TDataItem]:
        if datetime.last_value:
            start_dt = pendulum.parse(datetime.last_value)
        else:
            start_dt = pendulum.parse(start_date)

        if end_date is None:
            end_dt = pendulum.now("UTC")
        else:
            end_dt = pendulum.parse(datetime.end_value)

        base_params: Dict[str, Any] = {
            "type": "scheduled",
            "page_size": 300,
            "from": start_dt.to_date_string(),
            "to": end_dt.to_date_string(),
        }

        for user in client.get_users():
            user_id = user["id"]
            yield from client.get_meetings(user_id, base_params)

    @dlt.resource(write_disposition="merge", primary_key="id")
    def users() -> Iterable[TDataItem]:
        yield from client.get_users()

    @dlt.resource(write_disposition="merge", primary_key="id")
    def participants(
        datetime: dlt.sources.incremental[TAnyDateTime] = dlt.sources.incremental(
            "join_time",
            initial_value=start_date.isoformat(),
            end_value=end_date.isoformat() if end_date is not None else None,
            range_start="closed",
            range_end="closed",
        ),
    ) -> Iterable[TDataItem]:
        if datetime.last_value:
            start_dt = pendulum.parse(datetime.last_value)
        else:
            start_dt = pendulum.parse(start_date)

        if end_date is None:
            end_dt = pendulum.now("UTC")
        else:
            end_dt = pendulum.parse(datetime.end_value)

        participant_params: Dict[str, Any] = {
            "page_size": 300,
        }
        meeting_params = {
            "type": "previous_meetings",
            "page_size": 300,
        }
        for user in client.get_users():
            user_id = user["id"]
            for meeting in client.get_meetings(user_id=user_id, params=meeting_params):
                meeting_id = meeting["id"]
                yield from client.get_participants(
                    meeting_id=meeting_id,
                    params=participant_params,
                    start_date=start_dt,
                    end_date=end_dt,
                )

    return meetings, users, participants

"""Simple ClickUp source."""

from datetime import datetime
from typing import Iterable

import dlt
import pendulum
from dlt.common.time import ensure_pendulum_datetime
from dlt.sources import DltResource

from .helpers import ClickupClient


@dlt.source(max_table_nesting=0)
def clickup_source(
    api_token: str = dlt.secrets.value,
    start_date: datetime = None,
    end_date: datetime = None,
) -> Iterable[DltResource]:
    client = ClickupClient(api_token)

    @dlt.resource(
        name="user",
        primary_key="id",
        write_disposition="merge",
    )
    def user() -> Iterable[dict]:
        data = client.get("/user")
        yield data["user"]

    @dlt.resource(name="teams", primary_key="id", write_disposition="merge")
    def teams() -> Iterable[dict]:
        for team in client.get_teams():
            yield team

    @dlt.resource(name="spaces", primary_key="id", write_disposition="merge")
    def spaces() -> Iterable[dict]:
        for space in client.get_spaces():
            yield space

    @dlt.resource(name="lists", write_disposition="merge", primary_key="id")
    def lists() -> Iterable[dict]:
        for list in client.get_lists():
            yield list

    @dlt.resource(
        name="tasks",
        write_disposition="merge",
        primary_key="id",
        columns={"date_updated": {"data_type": "timestamp"}},
    )
    def tasks(
        date_updated: dlt.sources.incremental[str] = dlt.sources.incremental(
            "date_updated",
            initial_value=ensure_pendulum_datetime(start_date).in_timezone("UTC"),
            range_end="closed",
            range_start="closed",
        ),
    ) -> Iterable[dict]:
        if date_updated.last_value:
            start = ensure_pendulum_datetime(date_updated.last_value).in_timezone("UTC")
        else:
            start = ensure_pendulum_datetime(start_date).in_timezone("UTC")

        if date_updated.end_value is None:
            end = pendulum.now("UTC")
        else:
            end = date_updated.end_value.in_timezone("UTC")

        for list_obj in client.get_lists():
            for task in client.paginated(
                f"/list/{list_obj['id']}/task", "tasks", {"page_size": 100}
            ):
                task_dt = ensure_pendulum_datetime(int(task["date_updated"]) / 1000)
                if task_dt >= start and task_dt <= end:
                    task["date_updated"] = task_dt
                    yield task

    return (
        user,
        teams,
        spaces,
        lists,
        tasks,
    )

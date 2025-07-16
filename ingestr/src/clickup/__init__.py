"""Simple ClickUp source."""

from datetime import datetime
from typing import Iterable

import dlt
from dlt.common.time import ensure_pendulum_datetime
from dlt.sources import DltResource
import pendulum

from .helpers import ClickupClient


@dlt.source(max_table_nesting=0)
def clickup_source(api_token: str = dlt.secrets.value, start_date: datetime = None, end_date: datetime = None) -> Iterable[DltResource]:
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
        data = client.get("/team")
        yield from data.get("teams", [])

    @dlt.resource(name="spaces", primary_key="id", write_disposition="merge")
    def spaces() -> Iterable[dict]:
        for team in client.get("/team").get("teams", []):
            team_id = team["id"]
            yield from client.paginated(f"/team/{team_id}/space", "spaces")

    @dlt.resource(name="lists", write_disposition="merge", primary_key="id")
    def lists() -> Iterable[dict]:
        for team in client.get("/team").get("teams", []):
            team_id = team["id"]
            for space in client.paginated(f"/team/{team_id}/space", "spaces"):
                space_id = space["id"]
                yield from client.paginated(f"/space/{space_id}/list", "lists")

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
            end = (pendulum.now("UTC"))
        else:
            end = date_updated.end_value.in_timezone("UTC")
        
        for list_obj in lists():
            list_id = list_obj["id"]
            params = {"page_size": 100}
            for task in client.paginated(f"/list/{list_id}/task", "tasks", params):
                task_dt = ensure_pendulum_datetime(int(task["date_updated"]) / 1000)
                if task_dt >= start and task_dt <= end:
                    task["date_updated"] = task_dt
                    yield task

    return user, teams, lists, tasks, spaces

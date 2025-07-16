"""Simple ClickUp source."""

from typing import Iterable

import dlt
from dlt.common.time import ensure_pendulum_datetime
from dlt.sources import DltResource

from .helpers import ClickupClient


@dlt.source
def clickup_source(api_token: str = dlt.secrets.value) -> Iterable[DltResource]:
    client = ClickupClient(api_token)

    @dlt.resource(
        name="users",
        primary_key="id",
        write_disposition="merge",
    )
    def users() -> Iterable[dict]:
        data = client.get("/user")
        yield data

    @dlt.resource(name="teams", primary_key="id", write_disposition="merge")
    def teams() -> Iterable[dict]:
        data = client.get("/team")
        yield from data.get("teams", [])

    @dlt.resource(name="spaces", write_disposition="merge")
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
            initial_value="1970-01-01T00:00:00Z",
            range_end="closed",
            range_start="closed",
        ),
    ) -> Iterable[dict]:
        start = ensure_pendulum_datetime(
            date_updated.last_value or date_updated.initial_value
        )
        for list_obj in lists():
            list_id = list_obj["id"]
            params = {"page_size": 100}
            for task in client.paginated(f"/list/{list_id}/task", "tasks", params):
                task_dt = ensure_pendulum_datetime(int(task["date_updated"]) / 1000)
                if task_dt >= start:
                    task["date_updated"] = task_dt
                    yield task

    return users, teams, lists, tasks, spaces

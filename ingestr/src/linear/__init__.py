from typing import Any, Dict, Iterable, Iterator

import dlt
import pendulum

from .helpers import _normalize_issue, _normalize_team, _paginate

ISSUES_QUERY = """
query Issues($cursor: String) {
  issues(first: 50, after: $cursor) {
    nodes {
      id
      title
      description
      createdAt
      updatedAt
      creator { id }
      assignee { id}
      state { id}
      labels { nodes { id } }
      cycle { id}
      project { id }
      subtasks: children { nodes { id title } }
      comments(first: 250) { nodes { id body } }
      priority
      attachments { nodes { id } }
      subscribers { nodes { id } }
    }
    pageInfo { hasNextPage endCursor }
  }
}
"""

PROJECTS_QUERY = """
query Projects($cursor: String) {
  projects(first: 50, after: $cursor) {
    nodes {
      id
      name
      description
      createdAt
      updatedAt
      health
      priority
      targetDate
      lead { id }
    }
    pageInfo { hasNextPage endCursor }
  }
}
"""

TEAMS_QUERY = """
query Teams($cursor: String) {
  teams(first: 50, after: $cursor) {
    nodes {
      id
      name
      key
      description
      updatedAt
      createdAt
      memberships { nodes { id } }
      members { nodes { id } }
      projects { nodes { id } }
    }
    pageInfo { hasNextPage endCursor }
  }
}
"""

USERS_QUERY = """
query Users($cursor: String) {
  users(first: 50, after: $cursor) {
    nodes {
      id
      name
      displayName
      email
      createdAt
      updatedAt
    }
    pageInfo { hasNextPage endCursor }
  }
}
"""


@dlt.source(name="linear", max_table_nesting=0)
def linear_source(
    api_key: str,
    start_date: pendulum.DateTime,
    end_date: pendulum.DateTime | None = None,
) -> Iterable[dlt.sources.DltResource]:
    @dlt.resource(name="issues", primary_key="id", write_disposition="merge")
    def issues(
        updated_at: dlt.sources.incremental[str] = dlt.sources.incremental(
            "updatedAt",
            initial_value=start_date.isoformat(),
            end_value=end_date.isoformat() if end_date else None,
            range_start="closed",
            range_end="closed",
        ),
    ) -> Iterator[Dict[str, Any]]:
        if updated_at.last_value:
            current_start_date = pendulum.parse(updated_at.last_value)
        else:
            current_start_date = pendulum.parse(start_date)

        if updated_at.end_value:
            current_end_date = pendulum.parse(updated_at.end_value)
        else:
            current_end_date = pendulum.now(tz="UTC")

        for item in _paginate(api_key, ISSUES_QUERY, "issues"):
            if pendulum.parse(item["updatedAt"]) >= current_start_date:
                if pendulum.parse(item["updatedAt"]) <= current_end_date:
                    yield _normalize_issue(item)

    @dlt.resource(name="projects", primary_key="id", write_disposition="merge")
    def projects(
        updated_at: dlt.sources.incremental[str] = dlt.sources.incremental(
            "updatedAt",
            initial_value=start_date.isoformat(),
            end_value=end_date.isoformat() if end_date else None,
            range_start="closed",
            range_end="closed",
        ),
    ) -> Iterator[Dict[str, Any]]:
        if updated_at.last_value:
            current_start_date = pendulum.parse(updated_at.last_value)
        else:
            current_start_date = pendulum.parse(start_date)

        if updated_at.end_value:
            current_end_date = pendulum.parse(updated_at.end_value)
        else:
            current_end_date = pendulum.now(tz="UTC")

        for item in _paginate(api_key, PROJECTS_QUERY, "projects"):
            if pendulum.parse(item["updatedAt"]) >= current_start_date:
                if pendulum.parse(item["updatedAt"]) <= current_end_date:
                    yield item

    @dlt.resource(name="teams", primary_key="id", write_disposition="merge")
    def teams(
        updated_at: dlt.sources.incremental[str] = dlt.sources.incremental(
            "updatedAt",
            initial_value=start_date.isoformat(),
            end_value=end_date.isoformat() if end_date else None,
            range_start="closed",
            range_end="closed",
        ),
    ) -> Iterator[Dict[str, Any]]:
        print(start_date)
        if updated_at.last_value:
            current_start_date = pendulum.parse(updated_at.last_value)
        else:
            current_start_date = pendulum.parse(start_date)
        print(current_start_date)

        if updated_at.end_value:
            current_end_date = pendulum.parse(updated_at.end_value)
        else:
            current_end_date = pendulum.now(tz="UTC")

        for item in _paginate(api_key, TEAMS_QUERY, "teams"):
            if pendulum.parse(item["updatedAt"]) >= current_start_date:
                if pendulum.parse(item["updatedAt"]) <= current_end_date:
                    yield _normalize_team(item)

    @dlt.resource(name="users", primary_key="id", write_disposition="merge")
    def users(
        updated_at: dlt.sources.incremental[str] = dlt.sources.incremental(
            "updatedAt",
            initial_value=start_date.isoformat(),
            end_value=end_date.isoformat() if end_date else None,
            range_start="closed",
            range_end="closed",
        ),
    ) -> Iterator[Dict[str, Any]]:
        if updated_at.last_value:
            current_start_date = pendulum.parse(updated_at.last_value)
        else:
            current_start_date = pendulum.parse(start_date)

        if updated_at.end_value:
            current_end_date = pendulum.parse(updated_at.end_value)
        else:
            current_end_date = pendulum.now(tz="UTC")

        for item in _paginate(api_key, USERS_QUERY, "users"):
            if pendulum.parse(item["updatedAt"]) >= current_start_date:
                if pendulum.parse(item["updatedAt"]) <= current_end_date:
                    yield item

    return issues, projects, teams, users

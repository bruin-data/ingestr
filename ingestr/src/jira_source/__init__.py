# Copyright 2022-2025 ScaleVector
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#   http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""
This source provides data extraction from Jira Cloud via the REST API v3.

It defines several functions to fetch data from different parts of Jira including
projects, issues, users, boards, sprints, and various configuration objects like
issue types, statuses, and priorities.
"""

from typing import Any, Iterable, Optional

import dlt
from dlt.common.typing import TDataItem

from .helpers import get_client
from .settings import (
    DEFAULT_PAGE_SIZE,
    DEFAULT_START_DATE,
    ISSUE_FIELDS,
)


@dlt.source
def jira_source() -> Any:
    """
    The main function that runs all the other functions to fetch data from Jira.

    Returns:
        Sequence[DltResource]: A sequence of DltResource objects containing the fetched data.
    """
    return [
        projects,
        issues,
        users,
        issue_types,
        statuses,
        priorities,
        resolutions,
        project_versions,
        project_components,
        events,
    ]


@dlt.resource(write_disposition="replace")
def projects(
    base_url: str = dlt.secrets.value,
    email: str = dlt.secrets.value,
    api_token: str = dlt.secrets.value,
    expand: Optional[str] = None,
    recent: Optional[int] = None,
) -> Iterable[TDataItem]:
    """
    Fetches and returns a list of projects from Jira.

    Args:
        base_url (str): Jira instance URL (e.g., https://your-domain.atlassian.net)
        email (str): User email for authentication
        api_token (str): API token for authentication
        expand (str): Comma-separated list of fields to expand
        recent (int): Number of recent projects to return

    Yields:
        dict: The project data.
    """
    client = get_client(base_url, email, api_token)
    yield from client.get_projects(expand=expand, recent=recent)


@dlt.resource(
    write_disposition="merge",
    primary_key="id",
    max_table_nesting=2,
)
def issues(
    base_url: str = dlt.secrets.value,
    email: str = dlt.secrets.value,
    api_token: str = dlt.secrets.value,
    jql: str = "order by updated DESC",
    fields: Optional[str] = None,
    expand: Optional[str] = None,
    max_results: Optional[int] = None,
    updated: dlt.sources.incremental[str] = dlt.sources.incremental(
        "fields.updated",
        initial_value=DEFAULT_START_DATE,
        range_end="closed",
        range_start="closed",
    ),
) -> Iterable[TDataItem]:
    """
    Fetches issues from Jira using JQL search.

    Args:
        base_url (str): Jira instance URL
        email (str): User email for authentication
        api_token (str): API token for authentication
        jql (str): JQL query string
        fields (str): Comma-separated list of fields to return
        expand (str): Comma-separated list of fields to expand
        max_results (int): Maximum number of results to return
        updated (str): The date from which to fetch updated issues

    Yields:
        dict: The issue data.
    """
    client = get_client(base_url, email, api_token)

    # Build JQL with incremental filter
    incremental_jql = jql
    if updated.start_value:
        date_filter = f"updated >= '{updated.start_value}'"

        # Check if JQL has ORDER BY clause and handle it properly
        jql_upper = jql.upper()
        if "ORDER BY" in jql_upper:
            # Split at ORDER BY and add filter before it
            order_by_index = jql_upper.find("ORDER BY")
            main_query = jql[:order_by_index].strip()
            order_clause = jql[order_by_index:].strip()

            if main_query and (
                "WHERE" in main_query.upper()
                or "AND" in main_query.upper()
                or "OR" in main_query.upper()
            ):
                incremental_jql = f"({main_query}) AND {date_filter} {order_clause}"
            else:
                if main_query:
                    incremental_jql = f"{main_query} AND {date_filter} {order_clause}"
                else:
                    incremental_jql = f"{date_filter} {order_clause}"
        else:
            # No ORDER BY clause, use original logic
            if "WHERE" in jql_upper or "AND" in jql_upper or "OR" in jql_upper:
                incremental_jql = f"({jql}) AND {date_filter}"
            else:
                incremental_jql = f"{jql} AND {date_filter}"

    # Use default fields if not specified
    if fields is None:
        fields = ",".join(ISSUE_FIELDS)

    yield from client.search_issues(
        jql=incremental_jql, fields=fields, expand=expand, max_results=max_results
    )


@dlt.resource(write_disposition="replace")
def users(
    base_url: str = dlt.secrets.value,
    email: str = dlt.secrets.value,
    api_token: str = dlt.secrets.value,
    username: Optional[str] = None,
    account_id: Optional[str] = None,
    max_results: int = DEFAULT_PAGE_SIZE,
) -> Iterable[TDataItem]:
    """
    Fetches users from Jira.

    Args:
        base_url (str): Jira instance URL
        email (str): User email for authentication
        api_token (str): API token for authentication
        username (str): Username to search for
        account_id (str): Account ID to search for
        max_results (int): Maximum results per page

    Yields:
        dict: The user data.
    """
    client = get_client(base_url, email, api_token)
    yield from client.get_users(
        username=username, account_id=account_id, max_results=max_results
    )


@dlt.resource(write_disposition="replace")
def issue_types(
    base_url: str = dlt.secrets.value,
    email: str = dlt.secrets.value,
    api_token: str = dlt.secrets.value,
) -> Iterable[TDataItem]:
    """
    Fetches all issue types from Jira.

    Args:
        base_url (str): Jira instance URL
        email (str): User email for authentication
        api_token (str): API token for authentication

    Yields:
        dict: The issue type data.
    """
    client = get_client(base_url, email, api_token)
    yield from client.get_issue_types()


@dlt.resource(write_disposition="replace")
def statuses(
    base_url: str = dlt.secrets.value,
    email: str = dlt.secrets.value,
    api_token: str = dlt.secrets.value,
) -> Iterable[TDataItem]:
    """
    Fetches all statuses from Jira.

    Args:
        base_url (str): Jira instance URL
        email (str): User email for authentication
        api_token (str): API token for authentication

    Yields:
        dict: The status data.
    """
    client = get_client(base_url, email, api_token)
    yield from client.get_statuses()


@dlt.resource(write_disposition="replace")
def priorities(
    base_url: str = dlt.secrets.value,
    email: str = dlt.secrets.value,
    api_token: str = dlt.secrets.value,
) -> Iterable[TDataItem]:
    """
    Fetches all priorities from Jira.

    Args:
        base_url (str): Jira instance URL
        email (str): User email for authentication
        api_token (str): API token for authentication

    Yields:
        dict: The priority data.
    """
    client = get_client(base_url, email, api_token)
    yield from client.get_priorities()


@dlt.resource(write_disposition="replace")
def resolutions(
    base_url: str = dlt.secrets.value,
    email: str = dlt.secrets.value,
    api_token: str = dlt.secrets.value,
) -> Iterable[TDataItem]:
    """
    Fetches all resolutions from Jira.

    Args:
        base_url (str): Jira instance URL
        email (str): User email for authentication
        api_token (str): API token for authentication

    Yields:
        dict: The resolution data.
    """
    client = get_client(base_url, email, api_token)
    yield from client.get_resolutions()


@dlt.transformer(
    data_from=projects,
    write_disposition="replace",
)
@dlt.defer
def project_versions(
    project: TDataItem,
    base_url: str = dlt.secrets.value,
    email: str = dlt.secrets.value,
    api_token: str = dlt.secrets.value,
) -> Iterable[TDataItem]:
    """
    Fetches versions for each project from Jira.

    Args:
        project (dict): The project data.
        base_url (str): Jira instance URL
        email (str): User email for authentication
        api_token (str): API token for authentication

    Returns:
        list[dict]: The version data for the given project.
    """
    client = get_client(base_url, email, api_token)
    project_key = project.get("key")
    if not project_key:
        return []

    return list(client.get_project_versions(project_key))


@dlt.transformer(
    data_from=projects,
    write_disposition="replace",
)
@dlt.defer
def project_components(
    project: TDataItem,
    base_url: str = dlt.secrets.value,
    email: str = dlt.secrets.value,
    api_token: str = dlt.secrets.value,
) -> Iterable[TDataItem]:
    """
    Fetches components for each project from Jira.

    Args:
        project (dict): The project data.
        base_url (str): Jira instance URL
        email (str): User email for authentication
        api_token (str): API token for authentication

    Returns:
        list[dict]: The component data for the given project.
    """
    client = get_client(base_url, email, api_token)
    project_key = project.get("key")
    if not project_key:
        return []

    return list(client.get_project_components(project_key))


@dlt.resource(write_disposition="replace")
def events(
    base_url: str = dlt.secrets.value,
    email: str = dlt.secrets.value,
    api_token: str = dlt.secrets.value,
) -> Iterable[TDataItem]:
    """
    Fetches all event types from Jira (e.g., Issue Created, Issue Updated, etc.).

    Args:
        base_url (str): Jira instance URL
        email (str): User email for authentication
        api_token (str): API token for authentication

    Yields:
        dict: The event data.
    """
    client = get_client(base_url, email, api_token)
    yield from client.get_events()

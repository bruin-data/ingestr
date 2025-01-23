"""
This source provides data extraction from the Asana platform via their API.

It defines several functions to fetch data from different parts of Asana including
workspaces, projects, sections, tags, tasks, stories, teams, and users. These
functions are meant to be used as part of a data loading pipeline.
"""

import typing as t
from typing import Any, Iterable

import dlt
from dlt.common.typing import TDataItem

from .helpers import get_client
from .settings import (
    DEFAULT_START_DATE,
    PROJECT_FIELDS,
    REQUEST_TIMEOUT,
    SECTION_FIELDS,
    STORY_FIELDS,
    TAG_FIELDS,
    TASK_FIELDS,
    TEAMS_FIELD,
    USER_FIELDS,
    WORKSPACE_FIELDS,
)


@dlt.source
def asana_source() -> Any:  # should be Sequence[DltResource]:
    """
    The main function that runs all the other functions to fetch data from Asana.
    Returns:
        Sequence[DltResource]: A sequence of DltResource objects containing the fetched data.
    """
    return [
        workspaces,
        projects,
        sections,
        tags,
        tasks,
        stories,
        teams,
        users,
    ]


@dlt.resource(write_disposition="replace")
def workspaces(
    access_token: str = dlt.secrets.value, fields: Iterable[str] = WORKSPACE_FIELDS
) -> Iterable[TDataItem]:
    """
    Fetches and returns a list of workspaces from Asana.
    Args:
        access_token (str): The access token to authenticate the Asana API client, provided in the secrets file
        fields (Iterable[str]): The list of workspace fields to be retrieved from Asana API.
    Yields:
        dict: The workspace data.
    """
    yield from get_client(access_token).workspaces.find_all(opt_fields=",".join(fields))


@dlt.transformer(
    data_from=workspaces,
    write_disposition="replace",
)
@dlt.defer
def projects(
    workspace: TDataItem,
    access_token: str = dlt.secrets.value,
    fields: Iterable[str] = PROJECT_FIELDS,
) -> Iterable[TDataItem]:
    """
    Fetches and returns a list of projects for a given workspace from Asana.
    Args:
        workspace (dict): The workspace data.
        access_token (str): The access token to authenticate the Asana API client, provided in the secrets file
        fields (Iterable[str]): The list of workspace fields to be retrieved from Asana API.
    Returns:
        list[dict]: The project data for the given workspace.
    """
    return list(
        get_client(access_token).projects.find_all(
            workspace=workspace["gid"],
            timeout=REQUEST_TIMEOUT,
            opt_fields=",".join(fields),
        )
    )


@dlt.transformer(
    data_from=projects,
    write_disposition="replace",
)
@dlt.defer
def sections(
    project_array: t.List[TDataItem],
    access_token: str = dlt.secrets.value,
    fields: Iterable[str] = SECTION_FIELDS,
) -> Iterable[TDataItem]:
    """
    Fetches all sections for a given project from Asana.
    Args:
        project_array (list): The project data.
        access_token (str): The access token to authenticate the Asana API client, provided in the secrets file
        fields (Iterable[str]): The list of workspace fields to be retrieved from Asana API.
    Returns:
        list[dict]: The sections data for the given project.
    """
    return [
        section
        for project in project_array
        for section in get_client(access_token).sections.get_sections_for_project(
            project_gid=project["gid"],
            timeout=REQUEST_TIMEOUT,
            opt_fields=",".join(fields),
        )
    ]


@dlt.transformer(data_from=workspaces, write_disposition="replace")
@dlt.defer
def tags(
    workspace: TDataItem,
    access_token: str = dlt.secrets.value,
    fields: Iterable[str] = TAG_FIELDS,
) -> Iterable[TDataItem]:
    """
    Fetches all tags for a given workspace from Asana.
    Args:
        workspace (dict): The workspace data.
        access_token (str): The access token to authenticate the Asana API client, provided in the secrets file
        fields (Iterable[str]): The list of workspace fields to be retrieved from Asana API.
    Returns:
        list[dict]: The tags data for the given workspace.
    """
    return [
        tag
        for tag in get_client(access_token).tags.find_all(
            workspace=workspace["gid"],
            timeout=REQUEST_TIMEOUT,
            opt_fields=",".join(fields),
        )
    ]


@dlt.transformer(data_from=projects, write_disposition="merge", primary_key="gid")
def tasks(
    project_array: t.List[TDataItem],
    access_token: str = dlt.secrets.value,
    modified_at: dlt.sources.incremental[str] = dlt.sources.incremental(
        "modified_at",
        initial_value=DEFAULT_START_DATE,
        range_end="closed",
        range_start="closed",
    ),
    fields: Iterable[str] = TASK_FIELDS,
) -> Iterable[TDataItem]:
    """
    Fetches all tasks for a given project from Asana.
    Args:
        project_array (list): The project data.
        access_token (str): The access token to authenticate the Asana API client, provided in the secrets file

        modified_at (str): The date from which to fetch modified tasks.
        fields (Iterable[str]): The list of workspace fields to be retrieved from Asana API.
    Yields:
        dict: The task data for the given project.
    """
    yield from (
        task
        for project in project_array
        for task in get_client(access_token).tasks.find_all(
            project=project["gid"],
            timeout=REQUEST_TIMEOUT,
            modified_since=modified_at.start_value,
            opt_fields=",".join(fields),
        )
    )


@dlt.transformer(
    data_from=tasks,
    write_disposition="append",
)
@dlt.defer
def stories(
    task: TDataItem,
    access_token: str = dlt.secrets.value,
    fields: Iterable[str] = STORY_FIELDS,
) -> Iterable[TDataItem]:
    """
    Fetches stories for a task from Asana.
    Args:
        task (dict): The task data.
        access_token (str): The access token to authenticate the Asana API client, provided in the secrets file
        fields (Iterable[str]): The list of workspace fields to be retrieved from Asana API.
    Returns:
        list[dict]: The stories data for the given task.
    """
    return [
        story
        for story in get_client(access_token).stories.get_stories_for_task(
            task_gid=task["gid"],
            timeout=REQUEST_TIMEOUT,
            opt_fields=",".join(fields),
        )
    ]


@dlt.transformer(
    data_from=workspaces,
    write_disposition="replace",
)
@dlt.defer
def teams(
    workspace: TDataItem,
    access_token: str = dlt.secrets.value,
    fields: Iterable[str] = TEAMS_FIELD,
) -> Iterable[TDataItem]:
    """
    Fetches all teams for a given workspace from Asana.
    Args:
        workspace (dict): The workspace data.
        access_token (str): The access token to authenticate the Asana API client, provided in the secrets file
        fields (Iterable[str]): The list of workspace fields to be retrieved from Asana API.
    Returns:
        list[dict]: The teams data for the given workspace.
    """
    return [
        team
        for team in get_client(access_token).teams.find_by_organization(
            organization=workspace["gid"],
            timeout=REQUEST_TIMEOUT,
            opt_fields=",".join(fields),
        )
    ]


@dlt.transformer(
    data_from=workspaces,
    write_disposition="replace",
)
@dlt.defer
def users(
    workspace: TDataItem,
    access_token: str = dlt.secrets.value,
    fields: Iterable[str] = USER_FIELDS,
) -> Iterable[TDataItem]:
    """
    Fetches all users for a given workspace from Asana.
    Args:
        workspace (dict): The workspace data.
        access_token (str): The access token to authenticate the Asana API client, provided in the secrets file
        fields (Iterable[str]): The list of workspace fields to be retrieved from Asana API.
    Returns:
        list[dict]: The user data for the given workspace.
    """
    return [
        user
        for user in get_client(access_token).users.find_all(
            workspace=workspace["gid"],
            timeout=REQUEST_TIMEOUT,
            opt_fields=",".join(fields),
        )
    ]

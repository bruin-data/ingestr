"""Source that load github issues, pull requests and reactions for a specific repository via customizable graphql query. Loads events incrementally."""

import urllib.parse
from typing import Iterator, Optional, Sequence

import dlt
from dlt.common.typing import TDataItems
from dlt.sources import DltResource

from .helpers import get_reactions_data, get_rest_pages, get_stargazers


@dlt.source
def github_reactions(
    owner: str,
    name: str,
    access_token: str,
    items_per_page: int = 100,
    max_items: Optional[int] = None,
) -> Sequence[DltResource]:
    """Get reactions associated with issues, pull requests and comments in the repo `name` with owner `owner`.

    This source uses graphql to retrieve all issues (`issues` resource) and pull requests (`pull requests` resource) with the associated reactions (up to 100),
    comments (up to 100) and reactions to comments (also up to 100). Internally graphql is used to retrieve data. It is cost optimized and you are able to retrieve the
    data for fairly large repos quickly and cheaply.
    You can and should change the queries in `queries.py` to include for example additional fields or connections. The source can be hacked to add more resources for other
    repository nodes easily.

    Args:
        owner (str): The repository owner
        name (str): The repository name
        access_token (str): The classic access token. Will be injected from secrets if not provided.
        items_per_page (int, optional): How many issues/pull requests to get in single page. Defaults to 100.
        max_items (int, optional): How many issues/pull requests to get in total. None means All.
        max_item_age_seconds (float, optional): Do not get items older than this. Defaults to None. NOT IMPLEMENTED

    Returns:
        Sequence[DltResource]: Two DltResources: `issues` with issues and `pull_requests` with pull requests
    """
    return (
        dlt.resource(
            get_reactions_data(
                "issues",
                owner,
                name,
                access_token,
                items_per_page,
                max_items,
            ),
            name="issues",
            write_disposition="replace",
        ),
        dlt.resource(
            get_reactions_data(
                "pullRequests",
                owner,
                name,
                access_token,
                items_per_page,
                max_items,
            ),
            name="pull_requests",
            write_disposition="replace",
        ),
    )


@dlt.source(max_table_nesting=0)
def github_repo_events(
    owner: str, name: str, access_token: Optional[str] = None
) -> DltResource:
    """Gets events for repository `name` with owner `owner` incrementally.

    This source contains a single resource `repo_events` that gets given repository's events and dispatches them to separate tables with names based on event type.
    The data is loaded incrementally. Subsequent runs will get only new events and append them to tables.
    Please note that Github allows only for 300 events to be retrieved for public repositories. You should get the events frequently for the active repos.

    Args:
        owner (str): The repository owner
        name (str): The repository name
        access_token (str): The classic or fine-grained access token. If not provided, calls are made anonymously

    Returns:
        DltSource: source with the `repo_events` resource

    """

    # use naming function in table name to generate separate tables for each event
    @dlt.resource(primary_key="id", table_name=lambda i: i["type"])
    def repo_events(
        last_created_at: dlt.sources.incremental[str] = dlt.sources.incremental(
            "created_at",
            initial_value="1970-01-01T00:00:00Z",
            last_value_func=max,
            range_end="closed",
            range_start="closed",
        ),
    ) -> Iterator[TDataItems]:
        repos_path = (
            f"/repos/{urllib.parse.quote(owner)}/{urllib.parse.quote(name)}/events"
        )

        for page in get_rest_pages(access_token, repos_path + "?per_page=100"):
            yield page

            # stop requesting pages if the last element was already older than initial value
            # note: incremental will skip those items anyway, we just do not want to use the api limits
            if last_created_at.start_out_of_range:
                print(
                    f"Overlap with previous run created at {last_created_at.initial_value}"
                )
                break

    return repo_events


@dlt.source
def github_stargazers(
    owner: str,
    name: str,
    access_token: str,
    items_per_page: int = 100,
    max_items: Optional[int] = None,
) -> Sequence[DltResource]:
    """Get stargazers in the repo `name` with owner `owner`.

    This source uses graphql to retrieve all stargazers with the associated starred date,
    Internally graphql is used to retrieve data. It is cost optimized and you are able to retrieve the
    data for fairly large repos quickly and cheaply.

    Args:
        owner (str): The repository owner
        name (str): The repository name
        access_token (str): The classic access token. Will be injected from secrets if not provided.
        items_per_page (int, optional): How many issues/pull requests to get in single page. Defaults to 100.
        max_items (int, optional): How many issues/pull requests to get in total. None means All.

    Returns:
        Sequence[DltResource]: One DltResource: `stargazers`
    """
    return (
        dlt.resource(
            get_stargazers(
                owner,
                name,
                access_token,
                items_per_page,
                max_items,
            ),
            name="stargazers",
            write_disposition="replace",
        ),
    )

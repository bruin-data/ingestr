from typing import Iterator, List, Optional, Tuple

from dlt.common.typing import DictStrAny, StrAny
from dlt.common.utils import chunks
from dlt.sources.helpers import requests

from .queries import COMMENT_REACTIONS_QUERY, ISSUES_QUERY, RATE_LIMIT, STARGAZERS_QUERY
from .settings import GRAPHQL_API_BASE_URL, REST_API_BASE_URL


#
# Shared
#
def _get_auth_header(access_token: Optional[str]) -> StrAny:
    if access_token:
        return {"Authorization": f"Bearer {access_token}"}
    else:
        # REST API works without access token (with high rate limits)
        return {}


#
# Rest API helpers
#
def get_rest_pages(access_token: Optional[str], query: str) -> Iterator[List[StrAny]]:
    def _request(page_url: str) -> requests.Response:
        r = requests.get(page_url, headers=_get_auth_header(access_token))
        print(
            f"got page {page_url}, requests left: " + r.headers["x-ratelimit-remaining"]
        )
        return r

    next_page_url = REST_API_BASE_URL + query
    while True:
        r: requests.Response = _request(next_page_url)
        page_items = r.json()
        if len(page_items) == 0:
            break
        yield page_items
        if "next" not in r.links:
            break
        next_page_url = r.links["next"]["url"]


#
# GraphQL API helpers
#
def get_stargazers(
    owner: str,
    name: str,
    access_token: str,
    items_per_page: int,
    max_items: Optional[int],
) -> Iterator[Iterator[StrAny]]:
    variables = {"owner": owner, "name": name, "items_per_page": items_per_page}
    for page_items in _get_graphql_pages(
        access_token, STARGAZERS_QUERY, variables, "stargazers", max_items
    ):
        yield map(
            lambda item: {"starredAt": item["starredAt"], "user": item["node"]},
            page_items,
        )


def get_reactions_data(
    node_type: str,
    owner: str,
    name: str,
    access_token: str,
    items_per_page: int,
    max_items: Optional[int],
) -> Iterator[Iterator[StrAny]]:
    variables = {
        "owner": owner,
        "name": name,
        "issues_per_page": items_per_page,
        "first_reactions": 100,
        "first_comments": 100,
        "node_type": node_type,
    }
    for page_items in _get_graphql_pages(
        access_token, ISSUES_QUERY % node_type, variables, node_type, max_items
    ):
        # use reactionGroups to query for reactions to comments that have any reactions. reduces cost by 10-50x
        reacted_comment_ids = {}
        for item in page_items:
            for comment in item["comments"]["nodes"]:
                if any(group["createdAt"] for group in comment["reactionGroups"]):
                    # print(f"for comment {comment['id']}: has reaction")
                    reacted_comment_ids[comment["id"]] = comment
                # if "reactionGroups" in comment:
                comment.pop("reactionGroups", None)

        # get comment reactions by querying comment nodes separately
        comment_reactions = _get_comment_reaction(
            list(reacted_comment_ids.keys()), access_token
        )
        # attach the reaction nodes where they should be
        for comment in comment_reactions.values():
            comment_id = comment["id"]
            reacted_comment_ids[comment_id]["reactions"] = comment["reactions"]
        yield map(_extract_nested_nodes, page_items)


def _extract_top_connection(data: StrAny, node_type: str) -> StrAny:
    assert isinstance(data, dict) and len(data) == 1, (
        f"The data with list of {node_type} must be a dictionary and contain only one element"
    )
    data = next(iter(data.values()))
    return data[node_type]  # type: ignore


def _extract_nested_nodes(item: DictStrAny) -> DictStrAny:
    """Recursively moves `nodes` and `totalCount` to reduce nesting."""
    item["reactions_totalCount"] = item["reactions"].get("totalCount", 0)
    item["reactions"] = item["reactions"]["nodes"]
    comments = item["comments"]
    item["comments_totalCount"] = item["comments"].get("totalCount", 0)
    for comment in comments["nodes"]:
        if "reactions" in comment:
            comment["reactions_totalCount"] = comment["reactions"].get("totalCount", 0)
            comment["reactions"] = comment["reactions"]["nodes"]
    item["comments"] = comments["nodes"]
    return item


def _run_graphql_query(
    access_token: str, query: str, variables: DictStrAny
) -> Tuple[StrAny, StrAny]:
    def _request() -> requests.Response:
        r = requests.post(
            GRAPHQL_API_BASE_URL,
            json={"query": query, "variables": variables},
            headers=_get_auth_header(access_token),
        )
        return r

    data = _request().json()
    if "errors" in data:
        raise ValueError(data)
    data = data["data"]
    # pop rate limits
    rate_limit = data.pop("rateLimit", {"cost": 0, "remaining": 0})
    return data, rate_limit


def _get_graphql_pages(
    access_token: str, query: str, variables: DictStrAny, node_type: str, max_items: int
) -> Iterator[List[DictStrAny]]:
    items_count = 0
    while True:
        data, rate_limit = _run_graphql_query(access_token, query, variables)
        top_connection = _extract_top_connection(data, node_type)
        data_items = (
            top_connection["nodes"]
            if "nodes" in top_connection
            else top_connection["edges"]
        )
        items_count += len(data_items)
        print(
            f"Got {len(data_items)}/{items_count} {node_type}s, query cost {rate_limit['cost']}, remaining credits: {rate_limit['remaining']}"
        )
        if data_items:
            yield data_items
        else:
            return
        # print(data["repository"][node_type]["pageInfo"]["endCursor"])
        variables["page_after"] = _extract_top_connection(data, node_type)["pageInfo"][
            "endCursor"
        ]
        if max_items and items_count >= max_items:
            print(f"Max items limit reached: {items_count} >= {max_items}")
            return


def _get_comment_reaction(comment_ids: List[str], access_token: str) -> StrAny:
    """Builds a query from a list of comment nodes and returns associated reactions."""
    idx = 0
    data: DictStrAny = {}
    for page_chunk in chunks(comment_ids, 50):
        subs = []
        for comment_id in page_chunk:
            subs.append(COMMENT_REACTIONS_QUERY % (idx, comment_id))
            idx += 1
        subs.append(RATE_LIMIT)
        query = "{" + ",\n".join(subs) + "}"
        # print(query)
        page, rate_limit = _run_graphql_query(access_token, query, {})
        print(
            f"Got {len(page)} comments, query cost {rate_limit['cost']}, remaining credits: {rate_limit['remaining']}"
        )
        data.update(page)
    return data

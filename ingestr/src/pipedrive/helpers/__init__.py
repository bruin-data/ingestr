"""Pipedrive source helpers"""

from itertools import groupby
from typing import Any, Dict, Iterable, List, Tuple, cast  # noqa: F401

from dlt.common import pendulum  # noqa: F401


def _deals_flow_group_key(item: Dict[str, Any]) -> str:
    return item["object"]  # type: ignore[no-any-return]


def group_deal_flows(
    pages: Iterable[Iterable[Dict[str, Any]]],
) -> Iterable[Tuple[str, List[Dict[str, Any]]]]:
    for page in pages:
        for entity, items in groupby(
            sorted(page, key=_deals_flow_group_key), key=_deals_flow_group_key
        ):
            yield (
                entity,
                [dict(item["data"], timestamp=item["timestamp"]) for item in items],
            )

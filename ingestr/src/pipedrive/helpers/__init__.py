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

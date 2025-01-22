import json
from typing import Any, Iterator

import proto  # type: ignore
from dlt.common.typing import TDataItem


def to_dict(item: Any) -> Iterator[TDataItem]:
    """
    Processes a batch result (page of results per dimension) accordingly
    :param batch:
    :return:
    """
    return json.loads(
        proto.Message.to_json(
            item,
            preserving_proto_field_name=True,
            use_integers_for_enums=False,
            including_default_value_fields=False,
        )
    )

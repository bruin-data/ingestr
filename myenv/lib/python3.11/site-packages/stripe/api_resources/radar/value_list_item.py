# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.radar.value_list_item package is deprecated, please change your
    imports to import from stripe.radar directly.
    From:
      from stripe.api_resources.radar.value_list_item import ValueListItem
    To:
      from stripe.radar import ValueListItem
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe.radar._value_list_item import (  # noqa
        ValueListItem,
    )

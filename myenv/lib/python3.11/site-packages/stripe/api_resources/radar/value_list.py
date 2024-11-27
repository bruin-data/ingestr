# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.radar.value_list package is deprecated, please change your
    imports to import from stripe.radar directly.
    From:
      from stripe.api_resources.radar.value_list import ValueList
    To:
      from stripe.radar import ValueList
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe.radar._value_list import (  # noqa
        ValueList,
    )

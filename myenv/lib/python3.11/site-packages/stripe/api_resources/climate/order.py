# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.climate.order package is deprecated, please change your
    imports to import from stripe.climate directly.
    From:
      from stripe.api_resources.climate.order import Order
    To:
      from stripe.climate import Order
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe.climate._order import (  # noqa
        Order,
    )

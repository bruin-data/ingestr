# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.shipping_rate package is deprecated, please change your
    imports to import from stripe directly.
    From:
      from stripe.api_resources.shipping_rate import ShippingRate
    To:
      from stripe import ShippingRate
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe._shipping_rate import (  # noqa
        ShippingRate,
    )

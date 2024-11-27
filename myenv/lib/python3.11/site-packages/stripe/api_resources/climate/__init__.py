# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.climate package is deprecated, please change your
    imports to import from stripe.climate directly.
    From:
      from stripe.api_resources.climate import ...
    To:
      from stripe.climate import ...
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe.api_resources.climate.order import Order
    from stripe.api_resources.climate.product import Product
    from stripe.api_resources.climate.supplier import Supplier

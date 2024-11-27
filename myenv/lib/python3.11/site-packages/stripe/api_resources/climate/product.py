# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.climate.product package is deprecated, please change your
    imports to import from stripe.climate directly.
    From:
      from stripe.api_resources.climate.product import Product
    To:
      from stripe.climate import Product
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe.climate._product import (  # noqa
        Product,
    )

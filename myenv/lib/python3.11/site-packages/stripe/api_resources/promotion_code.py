# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.promotion_code package is deprecated, please change your
    imports to import from stripe directly.
    From:
      from stripe.api_resources.promotion_code import PromotionCode
    To:
      from stripe import PromotionCode
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe._promotion_code import (  # noqa
        PromotionCode,
    )

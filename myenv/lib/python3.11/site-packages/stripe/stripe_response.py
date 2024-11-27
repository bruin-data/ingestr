# -*- coding: utf-8 -*-
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.stripe_response package is deprecated, please change your
    imports to import from stripe directly.
    From:
      from stripe.stripe_response import StripeResponse
    To:
      from stripe import StripeResponse
    """,
    DeprecationWarning,
)

if not TYPE_CHECKING:
    from stripe._stripe_response import StripeResponse  # noqa: F401
    from stripe._stripe_response import StripeResponseBase  # noqa: F401
    from stripe._stripe_response import StripeStreamResponse  # noqa: F401

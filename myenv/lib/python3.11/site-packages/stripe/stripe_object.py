# -*- coding: utf-8 -*-
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.stripe_object package is deprecated, please change your
    imports to import from stripe directly.
    From:
      from stripe.stripe_object import StripeObject
    To:
      from stripe import StripeObject
    """,
    DeprecationWarning,
    stacklevel=2,
)

if not TYPE_CHECKING:
    from stripe._stripe_object import (  # noqa
        StripeObject,
    )

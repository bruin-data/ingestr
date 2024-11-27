# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.payment_link package is deprecated, please change your
    imports to import from stripe directly.
    From:
      from stripe.api_resources.payment_link import PaymentLink
    To:
      from stripe import PaymentLink
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe._payment_link import (  # noqa
        PaymentLink,
    )

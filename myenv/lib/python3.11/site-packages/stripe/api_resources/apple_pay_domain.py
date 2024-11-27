# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.apple_pay_domain package is deprecated, please change your
    imports to import from stripe directly.
    From:
      from stripe.api_resources.apple_pay_domain import ApplePayDomain
    To:
      from stripe import ApplePayDomain
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe._apple_pay_domain import (  # noqa
        ApplePayDomain,
    )

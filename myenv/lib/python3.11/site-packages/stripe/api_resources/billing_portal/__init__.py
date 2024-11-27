# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.billing_portal package is deprecated, please change your
    imports to import from stripe.billing_portal directly.
    From:
      from stripe.api_resources.billing_portal import ...
    To:
      from stripe.billing_portal import ...
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe.api_resources.billing_portal.configuration import Configuration
    from stripe.api_resources.billing_portal.session import Session

# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.checkout package is deprecated, please change your
    imports to import from stripe.checkout directly.
    From:
      from stripe.api_resources.checkout import ...
    To:
      from stripe.checkout import ...
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe.api_resources.checkout.session import Session

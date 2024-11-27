# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.forwarding package is deprecated, please change your
    imports to import from stripe.forwarding directly.
    From:
      from stripe.api_resources.forwarding import ...
    To:
      from stripe.forwarding import ...
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe.api_resources.forwarding.request import Request

# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.forwarding.request package is deprecated, please change your
    imports to import from stripe.forwarding directly.
    From:
      from stripe.api_resources.forwarding.request import Request
    To:
      from stripe.forwarding import Request
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe.forwarding._request import (  # noqa
        Request,
    )

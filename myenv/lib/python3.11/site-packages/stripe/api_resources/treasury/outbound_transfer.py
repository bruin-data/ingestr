# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.treasury.outbound_transfer package is deprecated, please change your
    imports to import from stripe.treasury directly.
    From:
      from stripe.api_resources.treasury.outbound_transfer import OutboundTransfer
    To:
      from stripe.treasury import OutboundTransfer
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe.treasury._outbound_transfer import (  # noqa
        OutboundTransfer,
    )

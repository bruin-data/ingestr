# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.treasury.received_credit package is deprecated, please change your
    imports to import from stripe.treasury directly.
    From:
      from stripe.api_resources.treasury.received_credit import ReceivedCredit
    To:
      from stripe.treasury import ReceivedCredit
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe.treasury._received_credit import (  # noqa
        ReceivedCredit,
    )

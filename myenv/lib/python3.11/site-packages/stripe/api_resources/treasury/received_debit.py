# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.treasury.received_debit package is deprecated, please change your
    imports to import from stripe.treasury directly.
    From:
      from stripe.api_resources.treasury.received_debit import ReceivedDebit
    To:
      from stripe.treasury import ReceivedDebit
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe.treasury._received_debit import (  # noqa
        ReceivedDebit,
    )

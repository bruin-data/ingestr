# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.financial_connections.account package is deprecated, please change your
    imports to import from stripe.financial_connections directly.
    From:
      from stripe.api_resources.financial_connections.account import Account
    To:
      from stripe.financial_connections import Account
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe.financial_connections._account import (  # noqa
        Account,
    )

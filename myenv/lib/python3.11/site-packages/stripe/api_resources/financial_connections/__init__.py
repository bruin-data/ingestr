# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.financial_connections package is deprecated, please change your
    imports to import from stripe.financial_connections directly.
    From:
      from stripe.api_resources.financial_connections import ...
    To:
      from stripe.financial_connections import ...
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe.api_resources.financial_connections.account import Account
    from stripe.api_resources.financial_connections.account_owner import (
        AccountOwner,
    )
    from stripe.api_resources.financial_connections.account_ownership import (
        AccountOwnership,
    )
    from stripe.api_resources.financial_connections.session import Session
    from stripe.api_resources.financial_connections.transaction import (
        Transaction,
    )

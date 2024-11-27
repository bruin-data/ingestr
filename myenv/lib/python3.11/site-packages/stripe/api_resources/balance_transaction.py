# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.balance_transaction package is deprecated, please change your
    imports to import from stripe directly.
    From:
      from stripe.api_resources.balance_transaction import BalanceTransaction
    To:
      from stripe import BalanceTransaction
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe._balance_transaction import (  # noqa
        BalanceTransaction,
    )

# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.bank_account package is deprecated, please change your
    imports to import from stripe directly.
    From:
      from stripe.api_resources.bank_account import BankAccount
    To:
      from stripe import BankAccount
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe._bank_account import (  # noqa
        BankAccount,
    )

# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.tax.transaction package is deprecated, please change your
    imports to import from stripe.tax directly.
    From:
      from stripe.api_resources.tax.transaction import Transaction
    To:
      from stripe.tax import Transaction
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe.tax._transaction import (  # noqa
        Transaction,
    )

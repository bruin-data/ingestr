# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.tax package is deprecated, please change your
    imports to import from stripe.tax directly.
    From:
      from stripe.api_resources.tax import ...
    To:
      from stripe.tax import ...
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe.api_resources.tax.calculation import Calculation
    from stripe.api_resources.tax.calculation_line_item import (
        CalculationLineItem,
    )
    from stripe.api_resources.tax.registration import Registration
    from stripe.api_resources.tax.settings import Settings
    from stripe.api_resources.tax.transaction import Transaction
    from stripe.api_resources.tax.transaction_line_item import (
        TransactionLineItem,
    )

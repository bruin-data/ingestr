# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.treasury.financial_account_features package is deprecated, please change your
    imports to import from stripe.treasury directly.
    From:
      from stripe.api_resources.treasury.financial_account_features import FinancialAccountFeatures
    To:
      from stripe.treasury import FinancialAccountFeatures
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe.treasury._financial_account_features import (  # noqa
        FinancialAccountFeatures,
    )

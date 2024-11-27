# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.funding_instructions package is deprecated, please change your
    imports to import from stripe directly.
    From:
      from stripe.api_resources.funding_instructions import FundingInstructions
    To:
      from stripe import FundingInstructions
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe._funding_instructions import (  # noqa
        FundingInstructions,
    )

# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.application_fee_refund package is deprecated, please change your
    imports to import from stripe directly.
    From:
      from stripe.api_resources.application_fee_refund import ApplicationFeeRefund
    To:
      from stripe import ApplicationFeeRefund
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe._application_fee_refund import (  # noqa
        ApplicationFeeRefund,
    )

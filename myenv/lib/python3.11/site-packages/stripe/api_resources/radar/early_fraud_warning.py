# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.radar.early_fraud_warning package is deprecated, please change your
    imports to import from stripe.radar directly.
    From:
      from stripe.api_resources.radar.early_fraud_warning import EarlyFraudWarning
    To:
      from stripe.radar import EarlyFraudWarning
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe.radar._early_fraud_warning import (  # noqa
        EarlyFraudWarning,
    )

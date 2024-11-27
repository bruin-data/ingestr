# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.radar package is deprecated, please change your
    imports to import from stripe.radar directly.
    From:
      from stripe.api_resources.radar import ...
    To:
      from stripe.radar import ...
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe.api_resources.radar.early_fraud_warning import (
        EarlyFraudWarning,
    )
    from stripe.api_resources.radar.value_list import ValueList
    from stripe.api_resources.radar.value_list_item import ValueListItem

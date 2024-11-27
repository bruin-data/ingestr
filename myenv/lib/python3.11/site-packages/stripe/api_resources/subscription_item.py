# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.subscription_item package is deprecated, please change your
    imports to import from stripe directly.
    From:
      from stripe.api_resources.subscription_item import SubscriptionItem
    To:
      from stripe import SubscriptionItem
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe._subscription_item import (  # noqa
        SubscriptionItem,
    )

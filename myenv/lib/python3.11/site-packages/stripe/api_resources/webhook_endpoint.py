# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.webhook_endpoint package is deprecated, please change your
    imports to import from stripe directly.
    From:
      from stripe.api_resources.webhook_endpoint import WebhookEndpoint
    To:
      from stripe import WebhookEndpoint
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe._webhook_endpoint import (  # noqa
        WebhookEndpoint,
    )

# -*- coding: utf-8 -*-
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.webhook package is deprecated, please change your
    imports to import from stripe directly.
    From:
      from stripe.webhook import Webhook
    To:
      from stripe import Webhook
    """,
    DeprecationWarning,
    stacklevel=2,
)

if not TYPE_CHECKING:
    from stripe._webhook import (  # noqa
        Webhook,
        WebhookSignature,
    )

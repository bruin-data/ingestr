# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.event package is deprecated, please change your
    imports to import from stripe directly.
    From:
      from stripe.api_resources.event import Event
    To:
      from stripe import Event
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe._event import (  # noqa
        Event,
    )

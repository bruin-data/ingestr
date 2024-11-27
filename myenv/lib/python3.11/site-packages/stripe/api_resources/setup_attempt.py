# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.setup_attempt package is deprecated, please change your
    imports to import from stripe directly.
    From:
      from stripe.api_resources.setup_attempt import SetupAttempt
    To:
      from stripe import SetupAttempt
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe._setup_attempt import (  # noqa
        SetupAttempt,
    )

# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.account_session package is deprecated, please change your
    imports to import from stripe directly.
    From:
      from stripe.api_resources.account_session import AccountSession
    To:
      from stripe import AccountSession
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe._account_session import (  # noqa
        AccountSession,
    )

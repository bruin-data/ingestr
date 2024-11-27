# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.account_link package is deprecated, please change your
    imports to import from stripe directly.
    From:
      from stripe.api_resources.account_link import AccountLink
    To:
      from stripe import AccountLink
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe._account_link import (  # noqa
        AccountLink,
    )

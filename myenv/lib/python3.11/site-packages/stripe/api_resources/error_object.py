# -*- coding: utf-8 -*-
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.error_object package is deprecated, please change your
    imports to import from stripe directly.
    From:
      from stripe.api_resources.error_object import ErrorObject
    To:
      from stripe import ErrorObject
    """,
    DeprecationWarning,
)
if not TYPE_CHECKING:
    from stripe._error_object import (  # noqa
        ErrorObject,
        OAuthErrorObject,
    )

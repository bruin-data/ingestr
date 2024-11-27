# -*- coding: utf-8 -*-
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.http_client package is deprecated, please change your
    imports to import from stripe directly.
    From:
      from stripe.http_client import HTTPClient
    To:
      from stripe import HTTPClient
    """,
    DeprecationWarning,
    stacklevel=2,
)

if not TYPE_CHECKING:
    from stripe._http_client import *  # noqa

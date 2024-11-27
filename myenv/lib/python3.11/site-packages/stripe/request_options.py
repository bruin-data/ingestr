# -*- coding: utf-8 -*-
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.request_options package is deprecated, please change your
    imports to import from stripe directly.
    From:
      from stripe.request_options import RequestOptions
    To:
      from stripe import RequestOptions
    """,
    DeprecationWarning,
)
__deprecated__ = ["RequestOptions"]
if not TYPE_CHECKING:
    from stripe._request_options import RequestOptions  # noqa

# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.custom_method package is deprecated, please change your
    imports to import from stripe directly.
    From:
      from stripe.api_resources.custom_method import custom_method
    To:
      from stripe import custom_method
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe._custom_method import (  # noqa
        custom_method,
    )

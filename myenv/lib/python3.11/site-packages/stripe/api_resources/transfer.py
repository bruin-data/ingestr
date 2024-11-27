# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.transfer package is deprecated, please change your
    imports to import from stripe directly.
    From:
      from stripe.api_resources.transfer import Transfer
    To:
      from stripe import Transfer
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe._transfer import (  # noqa
        Transfer,
    )

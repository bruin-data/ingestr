# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.verify_mixin package is deprecated, please change your
    imports to import from stripe directly.
    From:
      from stripe.api_resources.verify_mixin import VerifyMixin
    To:
      from stripe import VerifyMixin
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe._verify_mixin import (  # noqa
        VerifyMixin,
    )

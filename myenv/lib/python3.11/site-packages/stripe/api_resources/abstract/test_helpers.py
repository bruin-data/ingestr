# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.test_helpers package is deprecated, please change your
    imports to import from stripe directly.
    From:
      from stripe.api_resources.test_helpers import APIResourceTestHelpers
    To:
      from stripe import APIResourceTestHelpers
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe._test_helpers import (  # noqa
        APIResourceTestHelpers,
    )

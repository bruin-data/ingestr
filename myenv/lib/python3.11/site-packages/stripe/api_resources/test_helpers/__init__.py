# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.test_helpers package is deprecated, please change your
    imports to import from stripe.test_helpers directly.
    From:
      from stripe.api_resources.test_helpers import ...
    To:
      from stripe.test_helpers import ...
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe.api_resources.test_helpers.test_clock import TestClock

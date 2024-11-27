# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.entitlements.feature package is deprecated, please change your
    imports to import from stripe.entitlements directly.
    From:
      from stripe.api_resources.entitlements.feature import Feature
    To:
      from stripe.entitlements import Feature
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe.entitlements._feature import (  # noqa
        Feature,
    )

# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.entitlements package is deprecated, please change your
    imports to import from stripe.entitlements directly.
    From:
      from stripe.api_resources.entitlements import ...
    To:
      from stripe.entitlements import ...
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe.api_resources.entitlements.active_entitlement import (
        ActiveEntitlement,
    )
    from stripe.api_resources.entitlements.active_entitlement_summary import (
        ActiveEntitlementSummary,
    )
    from stripe.api_resources.entitlements.feature import Feature

# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.entitlements.active_entitlement package is deprecated, please change your
    imports to import from stripe.entitlements directly.
    From:
      from stripe.api_resources.entitlements.active_entitlement import ActiveEntitlement
    To:
      from stripe.entitlements import ActiveEntitlement
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe.entitlements._active_entitlement import (  # noqa
        ActiveEntitlement,
    )

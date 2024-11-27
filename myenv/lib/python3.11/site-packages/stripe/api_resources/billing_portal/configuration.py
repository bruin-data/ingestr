# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.billing_portal.configuration package is deprecated, please change your
    imports to import from stripe.billing_portal directly.
    From:
      from stripe.api_resources.billing_portal.configuration import Configuration
    To:
      from stripe.billing_portal import Configuration
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe.billing_portal._configuration import (  # noqa
        Configuration,
    )

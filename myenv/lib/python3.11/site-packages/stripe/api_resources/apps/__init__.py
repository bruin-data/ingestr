# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.apps package is deprecated, please change your
    imports to import from stripe.apps directly.
    From:
      from stripe.api_resources.apps import ...
    To:
      from stripe.apps import ...
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe.api_resources.apps.secret import Secret

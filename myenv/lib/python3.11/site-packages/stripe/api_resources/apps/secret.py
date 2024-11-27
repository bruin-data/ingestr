# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.apps.secret package is deprecated, please change your
    imports to import from stripe.apps directly.
    From:
      from stripe.api_resources.apps.secret import Secret
    To:
      from stripe.apps import Secret
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe.apps._secret import (  # noqa
        Secret,
    )

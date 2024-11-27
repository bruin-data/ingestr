# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.checkout.session package is deprecated, please change your
    imports to import from stripe.checkout directly.
    From:
      from stripe.api_resources.checkout.session import Session
    To:
      from stripe.checkout import Session
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe.checkout._session import (  # noqa
        Session,
    )

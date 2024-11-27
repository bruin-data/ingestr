# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.financial_connections.session package is deprecated, please change your
    imports to import from stripe.financial_connections directly.
    From:
      from stripe.api_resources.financial_connections.session import Session
    To:
      from stripe.financial_connections import Session
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe.financial_connections._session import (  # noqa
        Session,
    )

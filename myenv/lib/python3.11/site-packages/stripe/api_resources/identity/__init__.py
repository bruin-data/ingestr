# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.identity package is deprecated, please change your
    imports to import from stripe.identity directly.
    From:
      from stripe.api_resources.identity import ...
    To:
      from stripe.identity import ...
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe.api_resources.identity.verification_report import (
        VerificationReport,
    )
    from stripe.api_resources.identity.verification_session import (
        VerificationSession,
    )

# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.identity.verification_report package is deprecated, please change your
    imports to import from stripe.identity directly.
    From:
      from stripe.api_resources.identity.verification_report import VerificationReport
    To:
      from stripe.identity import VerificationReport
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe.identity._verification_report import (  # noqa
        VerificationReport,
    )

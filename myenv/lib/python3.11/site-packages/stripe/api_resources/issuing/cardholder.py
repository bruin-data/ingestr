# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.issuing.cardholder package is deprecated, please change your
    imports to import from stripe.issuing directly.
    From:
      from stripe.api_resources.issuing.cardholder import Cardholder
    To:
      from stripe.issuing import Cardholder
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe.issuing._cardholder import (  # noqa
        Cardholder,
    )

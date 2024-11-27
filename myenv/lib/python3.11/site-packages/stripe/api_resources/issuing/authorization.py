# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.issuing.authorization package is deprecated, please change your
    imports to import from stripe.issuing directly.
    From:
      from stripe.api_resources.issuing.authorization import Authorization
    To:
      from stripe.issuing import Authorization
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe.issuing._authorization import (  # noqa
        Authorization,
    )

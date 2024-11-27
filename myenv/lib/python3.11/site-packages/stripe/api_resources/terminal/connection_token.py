# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.terminal.connection_token package is deprecated, please change your
    imports to import from stripe.terminal directly.
    From:
      from stripe.api_resources.terminal.connection_token import ConnectionToken
    To:
      from stripe.terminal import ConnectionToken
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe.terminal._connection_token import (  # noqa
        ConnectionToken,
    )

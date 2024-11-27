# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.terminal package is deprecated, please change your
    imports to import from stripe.terminal directly.
    From:
      from stripe.api_resources.terminal import ...
    To:
      from stripe.terminal import ...
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe.api_resources.terminal.configuration import Configuration
    from stripe.api_resources.terminal.connection_token import ConnectionToken
    from stripe.api_resources.terminal.location import Location
    from stripe.api_resources.terminal.reader import Reader

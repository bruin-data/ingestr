# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.terminal.configuration package is deprecated, please change your
    imports to import from stripe.terminal directly.
    From:
      from stripe.api_resources.terminal.configuration import Configuration
    To:
      from stripe.terminal import Configuration
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe.terminal._configuration import (  # noqa
        Configuration,
    )

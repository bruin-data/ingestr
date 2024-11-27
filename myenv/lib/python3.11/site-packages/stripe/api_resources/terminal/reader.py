# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.terminal.reader package is deprecated, please change your
    imports to import from stripe.terminal directly.
    From:
      from stripe.api_resources.terminal.reader import Reader
    To:
      from stripe.terminal import Reader
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe.terminal._reader import (  # noqa
        Reader,
    )

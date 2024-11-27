# -*- coding: utf-8 -*-
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_version package is deprecated and will become internal in the future.
    """,
    DeprecationWarning,
)

if not TYPE_CHECKING:
    from stripe._api_version import (  # noqa
        _ApiVersion,
    )

# -*- coding: utf-8 -*-
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.version package is deprecated and will become internal in the future.
    Pleasse access the version via stripe.VERSION.
    """,
    DeprecationWarning,
)

if not TYPE_CHECKING:
    from stripe._version import (  # noqa
        VERSION,
    )

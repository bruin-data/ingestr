# -*- coding: utf-8 -*-
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.request_metrics package is deprecated and will become internal in the future.
    """,
    DeprecationWarning,
)

if not TYPE_CHECKING:
    from stripe._request_metrics import (  # noqa
        RequestMetrics,
    )

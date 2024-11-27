# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.billing.meter_event package is deprecated, please change your
    imports to import from stripe.billing directly.
    From:
      from stripe.api_resources.billing.meter_event import MeterEvent
    To:
      from stripe.billing import MeterEvent
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe.billing._meter_event import (  # noqa
        MeterEvent,
    )

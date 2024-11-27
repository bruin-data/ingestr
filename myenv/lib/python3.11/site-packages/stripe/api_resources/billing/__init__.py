# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.billing package is deprecated, please change your
    imports to import from stripe.billing directly.
    From:
      from stripe.api_resources.billing import ...
    To:
      from stripe.billing import ...
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe.api_resources.billing.alert import Alert
    from stripe.api_resources.billing.alert_triggered import AlertTriggered
    from stripe.api_resources.billing.meter import Meter
    from stripe.api_resources.billing.meter_event import MeterEvent
    from stripe.api_resources.billing.meter_event_adjustment import (
        MeterEventAdjustment,
    )
    from stripe.api_resources.billing.meter_event_summary import (
        MeterEventSummary,
    )

# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._stripe_service import StripeService
from stripe.billing._alert_service import AlertService
from stripe.billing._meter_event_adjustment_service import (
    MeterEventAdjustmentService,
)
from stripe.billing._meter_event_service import MeterEventService
from stripe.billing._meter_service import MeterService


class BillingService(StripeService):
    def __init__(self, requestor):
        super().__init__(requestor)
        self.alerts = AlertService(self._requestor)
        self.meters = MeterService(self._requestor)
        self.meter_events = MeterEventService(self._requestor)
        self.meter_event_adjustments = MeterEventAdjustmentService(
            self._requestor,
        )

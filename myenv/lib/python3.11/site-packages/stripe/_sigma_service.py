# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._stripe_service import StripeService
from stripe.sigma._scheduled_query_run_service import ScheduledQueryRunService


class SigmaService(StripeService):
    def __init__(self, requestor):
        super().__init__(requestor)
        self.scheduled_query_runs = ScheduledQueryRunService(self._requestor)

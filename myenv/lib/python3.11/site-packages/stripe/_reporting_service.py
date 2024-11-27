# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._stripe_service import StripeService
from stripe.reporting._report_run_service import ReportRunService
from stripe.reporting._report_type_service import ReportTypeService


class ReportingService(StripeService):
    def __init__(self, requestor):
        super().__init__(requestor)
        self.report_runs = ReportRunService(self._requestor)
        self.report_types = ReportTypeService(self._requestor)

# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._stripe_service import StripeService
from stripe.identity._verification_report_service import (
    VerificationReportService,
)
from stripe.identity._verification_session_service import (
    VerificationSessionService,
)


class IdentityService(StripeService):
    def __init__(self, requestor):
        super().__init__(requestor)
        self.verification_reports = VerificationReportService(self._requestor)
        self.verification_sessions = VerificationSessionService(
            self._requestor
        )

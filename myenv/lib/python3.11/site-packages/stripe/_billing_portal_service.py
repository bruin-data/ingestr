# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._stripe_service import StripeService
from stripe.billing_portal._configuration_service import ConfigurationService
from stripe.billing_portal._session_service import SessionService


class BillingPortalService(StripeService):
    def __init__(self, requestor):
        super().__init__(requestor)
        self.configurations = ConfigurationService(self._requestor)
        self.sessions = SessionService(self._requestor)

# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._stripe_service import StripeService
from stripe.checkout._session_service import SessionService


class CheckoutService(StripeService):
    def __init__(self, requestor):
        super().__init__(requestor)
        self.sessions = SessionService(self._requestor)

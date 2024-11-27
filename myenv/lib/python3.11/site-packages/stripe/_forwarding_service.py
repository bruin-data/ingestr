# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._stripe_service import StripeService
from stripe.forwarding._request_service import RequestService


class ForwardingService(StripeService):
    def __init__(self, requestor):
        super().__init__(requestor)
        self.requests = RequestService(self._requestor)

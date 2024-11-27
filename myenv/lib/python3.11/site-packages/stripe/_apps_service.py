# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._stripe_service import StripeService
from stripe.apps._secret_service import SecretService


class AppsService(StripeService):
    def __init__(self, requestor):
        super().__init__(requestor)
        self.secrets = SecretService(self._requestor)

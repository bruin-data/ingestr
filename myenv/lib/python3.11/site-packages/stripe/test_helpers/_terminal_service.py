# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._stripe_service import StripeService
from stripe.test_helpers.terminal._reader_service import ReaderService


class TerminalService(StripeService):
    def __init__(self, requestor):
        super().__init__(requestor)
        self.readers = ReaderService(self._requestor)

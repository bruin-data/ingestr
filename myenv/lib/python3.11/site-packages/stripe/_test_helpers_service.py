# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._stripe_service import StripeService
from stripe.test_helpers._confirmation_token_service import (
    ConfirmationTokenService,
)
from stripe.test_helpers._customer_service import CustomerService
from stripe.test_helpers._issuing_service import IssuingService
from stripe.test_helpers._refund_service import RefundService
from stripe.test_helpers._terminal_service import TerminalService
from stripe.test_helpers._test_clock_service import TestClockService
from stripe.test_helpers._treasury_service import TreasuryService


class TestHelpersService(StripeService):
    def __init__(self, requestor):
        super().__init__(requestor)
        self.confirmation_tokens = ConfirmationTokenService(self._requestor)
        self.customers = CustomerService(self._requestor)
        self.issuing = IssuingService(self._requestor)
        self.refunds = RefundService(self._requestor)
        self.terminal = TerminalService(self._requestor)
        self.test_clocks = TestClockService(self._requestor)
        self.treasury = TreasuryService(self._requestor)

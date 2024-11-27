# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._stripe_service import StripeService
from stripe.test_helpers.treasury._inbound_transfer_service import (
    InboundTransferService,
)
from stripe.test_helpers.treasury._outbound_payment_service import (
    OutboundPaymentService,
)
from stripe.test_helpers.treasury._outbound_transfer_service import (
    OutboundTransferService,
)
from stripe.test_helpers.treasury._received_credit_service import (
    ReceivedCreditService,
)
from stripe.test_helpers.treasury._received_debit_service import (
    ReceivedDebitService,
)


class TreasuryService(StripeService):
    def __init__(self, requestor):
        super().__init__(requestor)
        self.inbound_transfers = InboundTransferService(self._requestor)
        self.outbound_payments = OutboundPaymentService(self._requestor)
        self.outbound_transfers = OutboundTransferService(self._requestor)
        self.received_credits = ReceivedCreditService(self._requestor)
        self.received_debits = ReceivedDebitService(self._requestor)

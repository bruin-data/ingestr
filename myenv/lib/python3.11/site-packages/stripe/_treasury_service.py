# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._stripe_service import StripeService
from stripe.treasury._credit_reversal_service import CreditReversalService
from stripe.treasury._debit_reversal_service import DebitReversalService
from stripe.treasury._financial_account_service import FinancialAccountService
from stripe.treasury._inbound_transfer_service import InboundTransferService
from stripe.treasury._outbound_payment_service import OutboundPaymentService
from stripe.treasury._outbound_transfer_service import OutboundTransferService
from stripe.treasury._received_credit_service import ReceivedCreditService
from stripe.treasury._received_debit_service import ReceivedDebitService
from stripe.treasury._transaction_entry_service import TransactionEntryService
from stripe.treasury._transaction_service import TransactionService


class TreasuryService(StripeService):
    def __init__(self, requestor):
        super().__init__(requestor)
        self.credit_reversals = CreditReversalService(self._requestor)
        self.debit_reversals = DebitReversalService(self._requestor)
        self.financial_accounts = FinancialAccountService(self._requestor)
        self.inbound_transfers = InboundTransferService(self._requestor)
        self.outbound_payments = OutboundPaymentService(self._requestor)
        self.outbound_transfers = OutboundTransferService(self._requestor)
        self.received_credits = ReceivedCreditService(self._requestor)
        self.received_debits = ReceivedDebitService(self._requestor)
        self.transactions = TransactionService(self._requestor)
        self.transaction_entries = TransactionEntryService(self._requestor)

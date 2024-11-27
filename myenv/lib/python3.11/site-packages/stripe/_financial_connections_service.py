# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._stripe_service import StripeService
from stripe.financial_connections._account_service import AccountService
from stripe.financial_connections._session_service import SessionService
from stripe.financial_connections._transaction_service import (
    TransactionService,
)


class FinancialConnectionsService(StripeService):
    def __init__(self, requestor):
        super().__init__(requestor)
        self.accounts = AccountService(self._requestor)
        self.sessions = SessionService(self._requestor)
        self.transactions = TransactionService(self._requestor)

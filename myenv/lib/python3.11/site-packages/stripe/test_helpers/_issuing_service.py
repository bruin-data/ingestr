# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._stripe_service import StripeService
from stripe.test_helpers.issuing._authorization_service import (
    AuthorizationService,
)
from stripe.test_helpers.issuing._card_service import CardService
from stripe.test_helpers.issuing._personalization_design_service import (
    PersonalizationDesignService,
)
from stripe.test_helpers.issuing._transaction_service import TransactionService


class IssuingService(StripeService):
    def __init__(self, requestor):
        super().__init__(requestor)
        self.authorizations = AuthorizationService(self._requestor)
        self.cards = CardService(self._requestor)
        self.personalization_designs = PersonalizationDesignService(
            self._requestor,
        )
        self.transactions = TransactionService(self._requestor)

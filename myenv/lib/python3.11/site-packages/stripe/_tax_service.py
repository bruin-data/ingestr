# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._stripe_service import StripeService
from stripe.tax._calculation_service import CalculationService
from stripe.tax._registration_service import RegistrationService
from stripe.tax._settings_service import SettingsService
from stripe.tax._transaction_service import TransactionService


class TaxService(StripeService):
    def __init__(self, requestor):
        super().__init__(requestor)
        self.calculations = CalculationService(self._requestor)
        self.registrations = RegistrationService(self._requestor)
        self.settings = SettingsService(self._requestor)
        self.transactions = TransactionService(self._requestor)

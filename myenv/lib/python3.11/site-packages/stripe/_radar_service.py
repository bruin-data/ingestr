# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._stripe_service import StripeService
from stripe.radar._early_fraud_warning_service import EarlyFraudWarningService
from stripe.radar._value_list_item_service import ValueListItemService
from stripe.radar._value_list_service import ValueListService


class RadarService(StripeService):
    def __init__(self, requestor):
        super().__init__(requestor)
        self.early_fraud_warnings = EarlyFraudWarningService(self._requestor)
        self.value_lists = ValueListService(self._requestor)
        self.value_list_items = ValueListItemService(self._requestor)

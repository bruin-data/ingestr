# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._stripe_service import StripeService
from stripe.climate._order_service import OrderService
from stripe.climate._product_service import ProductService
from stripe.climate._supplier_service import SupplierService


class ClimateService(StripeService):
    def __init__(self, requestor):
        super().__init__(requestor)
        self.orders = OrderService(self._requestor)
        self.products = ProductService(self._requestor)
        self.suppliers = SupplierService(self._requestor)

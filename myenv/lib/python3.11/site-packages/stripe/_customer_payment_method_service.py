# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._payment_method import PaymentMethod
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from typing import List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class CustomerPaymentMethodService(StripeService):
    class ListParams(TypedDict):
        allow_redisplay: NotRequired[
            Literal["always", "limited", "unspecified"]
        ]
        """
        This field indicates whether this payment method can be shown again to its customer in a checkout flow. Stripe products such as Checkout and Elements use this field to determine whether a payment method can be shown as a saved payment method in a checkout flow. The field defaults to `unspecified`.
        """
        ending_before: NotRequired[str]
        """
        A cursor for use in pagination. `ending_before` is an object ID that defines your place in the list. For instance, if you make a list request and receive 100 objects, starting with `obj_bar`, your subsequent call can include `ending_before=obj_bar` in order to fetch the previous page of the list.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        limit: NotRequired[int]
        """
        A limit on the number of objects to be returned. Limit can range between 1 and 100, and the default is 10.
        """
        starting_after: NotRequired[str]
        """
        A cursor for use in pagination. `starting_after` is an object ID that defines your place in the list. For instance, if you make a list request and receive 100 objects, ending with `obj_foo`, your subsequent call can include `starting_after=obj_foo` in order to fetch the next page of the list.
        """
        type: NotRequired[
            Literal[
                "acss_debit",
                "affirm",
                "afterpay_clearpay",
                "alipay",
                "amazon_pay",
                "au_becs_debit",
                "bacs_debit",
                "bancontact",
                "blik",
                "boleto",
                "card",
                "cashapp",
                "customer_balance",
                "eps",
                "fpx",
                "giropay",
                "grabpay",
                "ideal",
                "klarna",
                "konbini",
                "link",
                "mobilepay",
                "multibanco",
                "oxxo",
                "p24",
                "paynow",
                "paypal",
                "pix",
                "promptpay",
                "revolut_pay",
                "sepa_debit",
                "sofort",
                "swish",
                "twint",
                "us_bank_account",
                "wechat_pay",
                "zip",
            ]
        ]
        """
        An optional filter on the list, based on the object `type` field. Without the filter, the list includes all current and future payment method types. If your integration expects only one type of payment method in the response, make sure to provide a type value in the request.
        """

    class RetrieveParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    def list(
        self,
        customer: str,
        params: "CustomerPaymentMethodService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[PaymentMethod]:
        """
        Returns a list of PaymentMethods for a given Customer
        """
        return cast(
            ListObject[PaymentMethod],
            self._request(
                "get",
                "/v1/customers/{customer}/payment_methods".format(
                    customer=sanitize_id(customer),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        customer: str,
        params: "CustomerPaymentMethodService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[PaymentMethod]:
        """
        Returns a list of PaymentMethods for a given Customer
        """
        return cast(
            ListObject[PaymentMethod],
            await self._request_async(
                "get",
                "/v1/customers/{customer}/payment_methods".format(
                    customer=sanitize_id(customer),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        customer: str,
        payment_method: str,
        params: "CustomerPaymentMethodService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> PaymentMethod:
        """
        Retrieves a PaymentMethod object for a given Customer.
        """
        return cast(
            PaymentMethod,
            self._request(
                "get",
                "/v1/customers/{customer}/payment_methods/{payment_method}".format(
                    customer=sanitize_id(customer),
                    payment_method=sanitize_id(payment_method),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        customer: str,
        payment_method: str,
        params: "CustomerPaymentMethodService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> PaymentMethod:
        """
        Retrieves a PaymentMethod object for a given Customer.
        """
        return cast(
            PaymentMethod,
            await self._request_async(
                "get",
                "/v1/customers/{customer}/payment_methods/{payment_method}".format(
                    customer=sanitize_id(customer),
                    payment_method=sanitize_id(payment_method),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

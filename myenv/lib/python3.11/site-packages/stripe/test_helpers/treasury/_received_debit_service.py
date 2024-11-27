# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe.treasury._received_debit import ReceivedDebit
from typing import List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class ReceivedDebitService(StripeService):
    class CreateParams(TypedDict):
        amount: int
        """
        Amount (in cents) to be transferred.
        """
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        description: NotRequired[str]
        """
        An arbitrary string attached to the object. Often useful for displaying to users.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        financial_account: str
        """
        The FinancialAccount to pull funds from.
        """
        initiating_payment_method_details: NotRequired[
            "ReceivedDebitService.CreateParamsInitiatingPaymentMethodDetails"
        ]
        """
        Initiating payment method details for the object.
        """
        network: Literal["ach"]
        """
        Specifies the network rails to be used. If not set, will default to the PaymentMethod's preferred network. See the [docs](https://stripe.com/docs/treasury/money-movement/timelines) to learn more about money movement timelines for each network type.
        """

    class CreateParamsInitiatingPaymentMethodDetails(TypedDict):
        type: Literal["us_bank_account"]
        """
        The source type.
        """
        us_bank_account: NotRequired[
            "ReceivedDebitService.CreateParamsInitiatingPaymentMethodDetailsUsBankAccount"
        ]
        """
        Optional fields for `us_bank_account`.
        """

    class CreateParamsInitiatingPaymentMethodDetailsUsBankAccount(TypedDict):
        account_holder_name: NotRequired[str]
        """
        The bank account holder's name.
        """
        account_number: NotRequired[str]
        """
        The bank account number.
        """
        routing_number: NotRequired[str]
        """
        The bank account's routing number.
        """

    def create(
        self,
        params: "ReceivedDebitService.CreateParams",
        options: RequestOptions = {},
    ) -> ReceivedDebit:
        """
        Use this endpoint to simulate a test mode ReceivedDebit initiated by a third party. In live mode, you can't directly create ReceivedDebits initiated by third parties.
        """
        return cast(
            ReceivedDebit,
            self._request(
                "post",
                "/v1/test_helpers/treasury/received_debits",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        params: "ReceivedDebitService.CreateParams",
        options: RequestOptions = {},
    ) -> ReceivedDebit:
        """
        Use this endpoint to simulate a test mode ReceivedDebit initiated by a third party. In live mode, you can't directly create ReceivedDebits initiated by third parties.
        """
        return cast(
            ReceivedDebit,
            await self._request_async(
                "post",
                "/v1/test_helpers/treasury/received_debits",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

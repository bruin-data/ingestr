# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from stripe.treasury._outbound_payment import OutboundPayment
from typing import Dict, List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class OutboundPaymentService(StripeService):
    class CancelParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class CreateParams(TypedDict):
        amount: int
        """
        Amount (in cents) to be transferred.
        """
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        customer: NotRequired[str]
        """
        ID of the customer to whom the OutboundPayment is sent. Must match the Customer attached to the `destination_payment_method` passed in.
        """
        description: NotRequired[str]
        """
        An arbitrary string attached to the object. Often useful for displaying to users.
        """
        destination_payment_method: NotRequired[str]
        """
        The PaymentMethod to use as the payment instrument for the OutboundPayment. Exclusive with `destination_payment_method_data`.
        """
        destination_payment_method_data: NotRequired[
            "OutboundPaymentService.CreateParamsDestinationPaymentMethodData"
        ]
        """
        Hash used to generate the PaymentMethod to be used for this OutboundPayment. Exclusive with `destination_payment_method`.
        """
        destination_payment_method_options: NotRequired[
            "OutboundPaymentService.CreateParamsDestinationPaymentMethodOptions"
        ]
        """
        Payment method-specific configuration for this OutboundPayment.
        """
        end_user_details: NotRequired[
            "OutboundPaymentService.CreateParamsEndUserDetails"
        ]
        """
        End user details.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        financial_account: str
        """
        The FinancialAccount to pull funds from.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        statement_descriptor: NotRequired[str]
        """
        The description that appears on the receiving end for this OutboundPayment (for example, bank statement for external bank transfer). Maximum 10 characters for `ach` payments, 140 characters for `us_domestic_wire` payments, or 500 characters for `stripe` network transfers. The default value is "payment".
        """

    class CreateParamsDestinationPaymentMethodData(TypedDict):
        billing_details: NotRequired[
            "OutboundPaymentService.CreateParamsDestinationPaymentMethodDataBillingDetails"
        ]
        """
        Billing information associated with the PaymentMethod that may be used or required by particular types of payment methods.
        """
        financial_account: NotRequired[str]
        """
        Required if type is set to `financial_account`. The FinancialAccount ID to send funds to.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        type: Literal["financial_account", "us_bank_account"]
        """
        The type of the PaymentMethod. An additional hash is included on the PaymentMethod with a name matching this value. It contains additional information specific to the PaymentMethod type.
        """
        us_bank_account: NotRequired[
            "OutboundPaymentService.CreateParamsDestinationPaymentMethodDataUsBankAccount"
        ]
        """
        Required hash if type is set to `us_bank_account`.
        """

    class CreateParamsDestinationPaymentMethodDataBillingDetails(TypedDict):
        address: NotRequired[
            "Literal['']|OutboundPaymentService.CreateParamsDestinationPaymentMethodDataBillingDetailsAddress"
        ]
        """
        Billing address.
        """
        email: NotRequired["Literal['']|str"]
        """
        Email address.
        """
        name: NotRequired["Literal['']|str"]
        """
        Full name.
        """
        phone: NotRequired["Literal['']|str"]
        """
        Billing phone number (including extension).
        """

    class CreateParamsDestinationPaymentMethodDataBillingDetailsAddress(
        TypedDict,
    ):
        city: NotRequired[str]
        """
        City, district, suburb, town, or village.
        """
        country: NotRequired[str]
        """
        Two-letter country code ([ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)).
        """
        line1: NotRequired[str]
        """
        Address line 1 (e.g., street, PO Box, or company name).
        """
        line2: NotRequired[str]
        """
        Address line 2 (e.g., apartment, suite, unit, or building).
        """
        postal_code: NotRequired[str]
        """
        ZIP or postal code.
        """
        state: NotRequired[str]
        """
        State, county, province, or region.
        """

    class CreateParamsDestinationPaymentMethodDataUsBankAccount(TypedDict):
        account_holder_type: NotRequired[Literal["company", "individual"]]
        """
        Account holder type: individual or company.
        """
        account_number: NotRequired[str]
        """
        Account number of the bank account.
        """
        account_type: NotRequired[Literal["checking", "savings"]]
        """
        Account type: checkings or savings. Defaults to checking if omitted.
        """
        financial_connections_account: NotRequired[str]
        """
        The ID of a Financial Connections Account to use as a payment method.
        """
        routing_number: NotRequired[str]
        """
        Routing number of the bank account.
        """

    class CreateParamsDestinationPaymentMethodOptions(TypedDict):
        us_bank_account: NotRequired[
            "Literal['']|OutboundPaymentService.CreateParamsDestinationPaymentMethodOptionsUsBankAccount"
        ]
        """
        Optional fields for `us_bank_account`.
        """

    class CreateParamsDestinationPaymentMethodOptionsUsBankAccount(TypedDict):
        network: NotRequired[Literal["ach", "us_domestic_wire"]]
        """
        Specifies the network rails to be used. If not set, will default to the PaymentMethod's preferred network. See the [docs](https://stripe.com/docs/treasury/money-movement/timelines) to learn more about money movement timelines for each network type.
        """

    class CreateParamsEndUserDetails(TypedDict):
        ip_address: NotRequired[str]
        """
        IP address of the user initiating the OutboundPayment. Must be supplied if `present` is set to `true`.
        """
        present: bool
        """
        `True` if the OutboundPayment creation request is being made on behalf of an end user by a platform. Otherwise, `false`.
        """

    class ListParams(TypedDict):
        created: NotRequired["OutboundPaymentService.ListParamsCreated|int"]
        """
        Only return OutboundPayments that were created during the given date interval.
        """
        customer: NotRequired[str]
        """
        Only return OutboundPayments sent to this customer.
        """
        ending_before: NotRequired[str]
        """
        A cursor for use in pagination. `ending_before` is an object ID that defines your place in the list. For instance, if you make a list request and receive 100 objects, starting with `obj_bar`, your subsequent call can include `ending_before=obj_bar` in order to fetch the previous page of the list.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        financial_account: str
        """
        Returns objects associated with this FinancialAccount.
        """
        limit: NotRequired[int]
        """
        A limit on the number of objects to be returned. Limit can range between 1 and 100, and the default is 10.
        """
        starting_after: NotRequired[str]
        """
        A cursor for use in pagination. `starting_after` is an object ID that defines your place in the list. For instance, if you make a list request and receive 100 objects, ending with `obj_foo`, your subsequent call can include `starting_after=obj_foo` in order to fetch the next page of the list.
        """
        status: NotRequired[
            Literal["canceled", "failed", "posted", "processing", "returned"]
        ]
        """
        Only return OutboundPayments that have the given status: `processing`, `failed`, `posted`, `returned`, or `canceled`.
        """

    class ListParamsCreated(TypedDict):
        gt: NotRequired[int]
        """
        Minimum value to filter by (exclusive)
        """
        gte: NotRequired[int]
        """
        Minimum value to filter by (inclusive)
        """
        lt: NotRequired[int]
        """
        Maximum value to filter by (exclusive)
        """
        lte: NotRequired[int]
        """
        Maximum value to filter by (inclusive)
        """

    class RetrieveParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    def list(
        self,
        params: "OutboundPaymentService.ListParams",
        options: RequestOptions = {},
    ) -> ListObject[OutboundPayment]:
        """
        Returns a list of OutboundPayments sent from the specified FinancialAccount.
        """
        return cast(
            ListObject[OutboundPayment],
            self._request(
                "get",
                "/v1/treasury/outbound_payments",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "OutboundPaymentService.ListParams",
        options: RequestOptions = {},
    ) -> ListObject[OutboundPayment]:
        """
        Returns a list of OutboundPayments sent from the specified FinancialAccount.
        """
        return cast(
            ListObject[OutboundPayment],
            await self._request_async(
                "get",
                "/v1/treasury/outbound_payments",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self,
        params: "OutboundPaymentService.CreateParams",
        options: RequestOptions = {},
    ) -> OutboundPayment:
        """
        Creates an OutboundPayment.
        """
        return cast(
            OutboundPayment,
            self._request(
                "post",
                "/v1/treasury/outbound_payments",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        params: "OutboundPaymentService.CreateParams",
        options: RequestOptions = {},
    ) -> OutboundPayment:
        """
        Creates an OutboundPayment.
        """
        return cast(
            OutboundPayment,
            await self._request_async(
                "post",
                "/v1/treasury/outbound_payments",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        id: str,
        params: "OutboundPaymentService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> OutboundPayment:
        """
        Retrieves the details of an existing OutboundPayment by passing the unique OutboundPayment ID from either the OutboundPayment creation request or OutboundPayment list.
        """
        return cast(
            OutboundPayment,
            self._request(
                "get",
                "/v1/treasury/outbound_payments/{id}".format(
                    id=sanitize_id(id),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        id: str,
        params: "OutboundPaymentService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> OutboundPayment:
        """
        Retrieves the details of an existing OutboundPayment by passing the unique OutboundPayment ID from either the OutboundPayment creation request or OutboundPayment list.
        """
        return cast(
            OutboundPayment,
            await self._request_async(
                "get",
                "/v1/treasury/outbound_payments/{id}".format(
                    id=sanitize_id(id),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def cancel(
        self,
        id: str,
        params: "OutboundPaymentService.CancelParams" = {},
        options: RequestOptions = {},
    ) -> OutboundPayment:
        """
        Cancel an OutboundPayment.
        """
        return cast(
            OutboundPayment,
            self._request(
                "post",
                "/v1/treasury/outbound_payments/{id}/cancel".format(
                    id=sanitize_id(id),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def cancel_async(
        self,
        id: str,
        params: "OutboundPaymentService.CancelParams" = {},
        options: RequestOptions = {},
    ) -> OutboundPayment:
        """
        Cancel an OutboundPayment.
        """
        return cast(
            OutboundPayment,
            await self._request_async(
                "post",
                "/v1/treasury/outbound_payments/{id}/cancel".format(
                    id=sanitize_id(id),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from stripe.treasury._outbound_transfer import OutboundTransfer
from typing import Dict, List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class OutboundTransferService(StripeService):
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
        description: NotRequired[str]
        """
        An arbitrary string attached to the object. Often useful for displaying to users.
        """
        destination_payment_method: NotRequired[str]
        """
        The PaymentMethod to use as the payment instrument for the OutboundTransfer.
        """
        destination_payment_method_options: NotRequired[
            "OutboundTransferService.CreateParamsDestinationPaymentMethodOptions"
        ]
        """
        Hash describing payment method configuration details.
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
        Statement descriptor to be shown on the receiving end of an OutboundTransfer. Maximum 10 characters for `ach` transfers or 140 characters for `us_domestic_wire` transfers. The default value is "transfer".
        """

    class CreateParamsDestinationPaymentMethodOptions(TypedDict):
        us_bank_account: NotRequired[
            "Literal['']|OutboundTransferService.CreateParamsDestinationPaymentMethodOptionsUsBankAccount"
        ]
        """
        Optional fields for `us_bank_account`.
        """

    class CreateParamsDestinationPaymentMethodOptionsUsBankAccount(TypedDict):
        network: NotRequired[Literal["ach", "us_domestic_wire"]]
        """
        Specifies the network rails to be used. If not set, will default to the PaymentMethod's preferred network. See the [docs](https://stripe.com/docs/treasury/money-movement/timelines) to learn more about money movement timelines for each network type.
        """

    class ListParams(TypedDict):
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
        Only return OutboundTransfers that have the given status: `processing`, `canceled`, `failed`, `posted`, or `returned`.
        """

    class RetrieveParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    def list(
        self,
        params: "OutboundTransferService.ListParams",
        options: RequestOptions = {},
    ) -> ListObject[OutboundTransfer]:
        """
        Returns a list of OutboundTransfers sent from the specified FinancialAccount.
        """
        return cast(
            ListObject[OutboundTransfer],
            self._request(
                "get",
                "/v1/treasury/outbound_transfers",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "OutboundTransferService.ListParams",
        options: RequestOptions = {},
    ) -> ListObject[OutboundTransfer]:
        """
        Returns a list of OutboundTransfers sent from the specified FinancialAccount.
        """
        return cast(
            ListObject[OutboundTransfer],
            await self._request_async(
                "get",
                "/v1/treasury/outbound_transfers",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self,
        params: "OutboundTransferService.CreateParams",
        options: RequestOptions = {},
    ) -> OutboundTransfer:
        """
        Creates an OutboundTransfer.
        """
        return cast(
            OutboundTransfer,
            self._request(
                "post",
                "/v1/treasury/outbound_transfers",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        params: "OutboundTransferService.CreateParams",
        options: RequestOptions = {},
    ) -> OutboundTransfer:
        """
        Creates an OutboundTransfer.
        """
        return cast(
            OutboundTransfer,
            await self._request_async(
                "post",
                "/v1/treasury/outbound_transfers",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        outbound_transfer: str,
        params: "OutboundTransferService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> OutboundTransfer:
        """
        Retrieves the details of an existing OutboundTransfer by passing the unique OutboundTransfer ID from either the OutboundTransfer creation request or OutboundTransfer list.
        """
        return cast(
            OutboundTransfer,
            self._request(
                "get",
                "/v1/treasury/outbound_transfers/{outbound_transfer}".format(
                    outbound_transfer=sanitize_id(outbound_transfer),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        outbound_transfer: str,
        params: "OutboundTransferService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> OutboundTransfer:
        """
        Retrieves the details of an existing OutboundTransfer by passing the unique OutboundTransfer ID from either the OutboundTransfer creation request or OutboundTransfer list.
        """
        return cast(
            OutboundTransfer,
            await self._request_async(
                "get",
                "/v1/treasury/outbound_transfers/{outbound_transfer}".format(
                    outbound_transfer=sanitize_id(outbound_transfer),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def cancel(
        self,
        outbound_transfer: str,
        params: "OutboundTransferService.CancelParams" = {},
        options: RequestOptions = {},
    ) -> OutboundTransfer:
        """
        An OutboundTransfer can be canceled if the funds have not yet been paid out.
        """
        return cast(
            OutboundTransfer,
            self._request(
                "post",
                "/v1/treasury/outbound_transfers/{outbound_transfer}/cancel".format(
                    outbound_transfer=sanitize_id(outbound_transfer),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def cancel_async(
        self,
        outbound_transfer: str,
        params: "OutboundTransferService.CancelParams" = {},
        options: RequestOptions = {},
    ) -> OutboundTransfer:
        """
        An OutboundTransfer can be canceled if the funds have not yet been paid out.
        """
        return cast(
            OutboundTransfer,
            await self._request_async(
                "post",
                "/v1/treasury/outbound_transfers/{outbound_transfer}/cancel".format(
                    outbound_transfer=sanitize_id(outbound_transfer),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

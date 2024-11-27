# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from stripe.treasury._inbound_transfer import InboundTransfer
from typing import Dict, List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class InboundTransferService(StripeService):
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
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        financial_account: str
        """
        The FinancialAccount to send funds to.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        origin_payment_method: str
        """
        The origin payment method to be debited for the InboundTransfer.
        """
        statement_descriptor: NotRequired[str]
        """
        The complete description that appears on your customers' statements. Maximum 10 characters.
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
            Literal["canceled", "failed", "processing", "succeeded"]
        ]
        """
        Only return InboundTransfers that have the given status: `processing`, `succeeded`, `failed` or `canceled`.
        """

    class RetrieveParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    def list(
        self,
        params: "InboundTransferService.ListParams",
        options: RequestOptions = {},
    ) -> ListObject[InboundTransfer]:
        """
        Returns a list of InboundTransfers sent from the specified FinancialAccount.
        """
        return cast(
            ListObject[InboundTransfer],
            self._request(
                "get",
                "/v1/treasury/inbound_transfers",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "InboundTransferService.ListParams",
        options: RequestOptions = {},
    ) -> ListObject[InboundTransfer]:
        """
        Returns a list of InboundTransfers sent from the specified FinancialAccount.
        """
        return cast(
            ListObject[InboundTransfer],
            await self._request_async(
                "get",
                "/v1/treasury/inbound_transfers",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self,
        params: "InboundTransferService.CreateParams",
        options: RequestOptions = {},
    ) -> InboundTransfer:
        """
        Creates an InboundTransfer.
        """
        return cast(
            InboundTransfer,
            self._request(
                "post",
                "/v1/treasury/inbound_transfers",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        params: "InboundTransferService.CreateParams",
        options: RequestOptions = {},
    ) -> InboundTransfer:
        """
        Creates an InboundTransfer.
        """
        return cast(
            InboundTransfer,
            await self._request_async(
                "post",
                "/v1/treasury/inbound_transfers",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        id: str,
        params: "InboundTransferService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> InboundTransfer:
        """
        Retrieves the details of an existing InboundTransfer.
        """
        return cast(
            InboundTransfer,
            self._request(
                "get",
                "/v1/treasury/inbound_transfers/{id}".format(
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
        params: "InboundTransferService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> InboundTransfer:
        """
        Retrieves the details of an existing InboundTransfer.
        """
        return cast(
            InboundTransfer,
            await self._request_async(
                "get",
                "/v1/treasury/inbound_transfers/{id}".format(
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
        inbound_transfer: str,
        params: "InboundTransferService.CancelParams" = {},
        options: RequestOptions = {},
    ) -> InboundTransfer:
        """
        Cancels an InboundTransfer.
        """
        return cast(
            InboundTransfer,
            self._request(
                "post",
                "/v1/treasury/inbound_transfers/{inbound_transfer}/cancel".format(
                    inbound_transfer=sanitize_id(inbound_transfer),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def cancel_async(
        self,
        inbound_transfer: str,
        params: "InboundTransferService.CancelParams" = {},
        options: RequestOptions = {},
    ) -> InboundTransfer:
        """
        Cancels an InboundTransfer.
        """
        return cast(
            InboundTransfer,
            await self._request_async(
                "post",
                "/v1/treasury/inbound_transfers/{inbound_transfer}/cancel".format(
                    inbound_transfer=sanitize_id(inbound_transfer),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

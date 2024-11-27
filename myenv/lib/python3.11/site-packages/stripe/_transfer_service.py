# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._transfer import Transfer
from stripe._transfer_reversal_service import TransferReversalService
from stripe._util import sanitize_id
from typing import Dict, List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class TransferService(StripeService):
    def __init__(self, requestor):
        super().__init__(requestor)
        self.reversals = TransferReversalService(self._requestor)

    class CreateParams(TypedDict):
        amount: NotRequired[int]
        """
        A positive integer in cents (or local equivalent) representing how much to transfer.
        """
        currency: str
        """
        Three-letter [ISO code for currency](https://www.iso.org/iso-4217-currency-codes.html) in lowercase. Must be a [supported currency](https://docs.stripe.com/currencies).
        """
        description: NotRequired[str]
        """
        An arbitrary string attached to the object. Often useful for displaying to users.
        """
        destination: str
        """
        The ID of a connected Stripe account. [See the Connect documentation](https://stripe.com/docs/connect/separate-charges-and-transfers) for details.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        source_transaction: NotRequired[str]
        """
        You can use this parameter to transfer funds from a charge before they are added to your available balance. A pending balance will transfer immediately but the funds will not become available until the original charge becomes available. [See the Connect documentation](https://stripe.com/docs/connect/separate-charges-and-transfers#transfer-availability) for details.
        """
        source_type: NotRequired[Literal["bank_account", "card", "fpx"]]
        """
        The source balance to use for this transfer. One of `bank_account`, `card`, or `fpx`. For most users, this will default to `card`.
        """
        transfer_group: NotRequired[str]
        """
        A string that identifies this transaction as part of a group. See the [Connect documentation](https://stripe.com/docs/connect/separate-charges-and-transfers#transfer-options) for details.
        """

    class ListParams(TypedDict):
        created: NotRequired["TransferService.ListParamsCreated|int"]
        """
        Only return transfers that were created during the given date interval.
        """
        destination: NotRequired[str]
        """
        Only return transfers for the destination specified by this account ID.
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
        transfer_group: NotRequired[str]
        """
        Only return transfers with the specified transfer group.
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

    class UpdateParams(TypedDict):
        description: NotRequired[str]
        """
        An arbitrary string attached to the object. Often useful for displaying to users.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """

    def list(
        self,
        params: "TransferService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Transfer]:
        """
        Returns a list of existing transfers sent to connected accounts. The transfers are returned in sorted order, with the most recently created transfers appearing first.
        """
        return cast(
            ListObject[Transfer],
            self._request(
                "get",
                "/v1/transfers",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "TransferService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Transfer]:
        """
        Returns a list of existing transfers sent to connected accounts. The transfers are returned in sorted order, with the most recently created transfers appearing first.
        """
        return cast(
            ListObject[Transfer],
            await self._request_async(
                "get",
                "/v1/transfers",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self,
        params: "TransferService.CreateParams",
        options: RequestOptions = {},
    ) -> Transfer:
        """
        To send funds from your Stripe account to a connected account, you create a new transfer object. Your [Stripe balance](https://stripe.com/docs/api#balance) must be able to cover the transfer amount, or you'll receive an “Insufficient Funds” error.
        """
        return cast(
            Transfer,
            self._request(
                "post",
                "/v1/transfers",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        params: "TransferService.CreateParams",
        options: RequestOptions = {},
    ) -> Transfer:
        """
        To send funds from your Stripe account to a connected account, you create a new transfer object. Your [Stripe balance](https://stripe.com/docs/api#balance) must be able to cover the transfer amount, or you'll receive an “Insufficient Funds” error.
        """
        return cast(
            Transfer,
            await self._request_async(
                "post",
                "/v1/transfers",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        transfer: str,
        params: "TransferService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Transfer:
        """
        Retrieves the details of an existing transfer. Supply the unique transfer ID from either a transfer creation request or the transfer list, and Stripe will return the corresponding transfer information.
        """
        return cast(
            Transfer,
            self._request(
                "get",
                "/v1/transfers/{transfer}".format(
                    transfer=sanitize_id(transfer),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        transfer: str,
        params: "TransferService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Transfer:
        """
        Retrieves the details of an existing transfer. Supply the unique transfer ID from either a transfer creation request or the transfer list, and Stripe will return the corresponding transfer information.
        """
        return cast(
            Transfer,
            await self._request_async(
                "get",
                "/v1/transfers/{transfer}".format(
                    transfer=sanitize_id(transfer),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def update(
        self,
        transfer: str,
        params: "TransferService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Transfer:
        """
        Updates the specified transfer by setting the values of the parameters passed. Any parameters not provided will be left unchanged.

        This request accepts only metadata as an argument.
        """
        return cast(
            Transfer,
            self._request(
                "post",
                "/v1/transfers/{transfer}".format(
                    transfer=sanitize_id(transfer),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def update_async(
        self,
        transfer: str,
        params: "TransferService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Transfer:
        """
        Updates the specified transfer by setting the values of the parameters passed. Any parameters not provided will be left unchanged.

        This request accepts only metadata as an argument.
        """
        return cast(
            Transfer,
            await self._request_async(
                "post",
                "/v1/transfers/{transfer}".format(
                    transfer=sanitize_id(transfer),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._payout import Payout
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from typing import Dict, List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class PayoutService(StripeService):
    class CancelParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class CreateParams(TypedDict):
        amount: int
        """
        A positive integer in cents representing how much to payout.
        """
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        description: NotRequired[str]
        """
        An arbitrary string attached to the object. Often useful for displaying to users.
        """
        destination: NotRequired[str]
        """
        The ID of a bank account or a card to send the payout to. If you don't provide a destination, we use the default external account for the specified currency.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        method: NotRequired[Literal["instant", "standard"]]
        """
        The method used to send this payout, which is `standard` or `instant`. We support `instant` for payouts to debit cards and bank accounts in certain countries. Learn more about [bank support for Instant Payouts](https://stripe.com/docs/payouts/instant-payouts-banks).
        """
        source_type: NotRequired[Literal["bank_account", "card", "fpx"]]
        """
        The balance type of your Stripe balance to draw this payout from. Balances for different payment sources are kept separately. You can find the amounts with the Balances API. One of `bank_account`, `card`, or `fpx`.
        """
        statement_descriptor: NotRequired[str]
        """
        A string that displays on the recipient's bank or card statement (up to 22 characters). A `statement_descriptor` that's longer than 22 characters return an error. Most banks truncate this information and display it inconsistently. Some banks might not display it at all.
        """

    class ListParams(TypedDict):
        arrival_date: NotRequired["PayoutService.ListParamsArrivalDate|int"]
        """
        Only return payouts that are expected to arrive during the given date interval.
        """
        created: NotRequired["PayoutService.ListParamsCreated|int"]
        """
        Only return payouts that were created during the given date interval.
        """
        destination: NotRequired[str]
        """
        The ID of an external account - only return payouts sent to this external account.
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
        status: NotRequired[str]
        """
        Only return payouts that have the given status: `pending`, `paid`, `failed`, or `canceled`.
        """

    class ListParamsArrivalDate(TypedDict):
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

    class ReverseParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """

    class UpdateParams(TypedDict):
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
        params: "PayoutService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Payout]:
        """
        Returns a list of existing payouts sent to third-party bank accounts or payouts that Stripe sent to you. The payouts return in sorted order, with the most recently created payouts appearing first.
        """
        return cast(
            ListObject[Payout],
            self._request(
                "get",
                "/v1/payouts",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "PayoutService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Payout]:
        """
        Returns a list of existing payouts sent to third-party bank accounts or payouts that Stripe sent to you. The payouts return in sorted order, with the most recently created payouts appearing first.
        """
        return cast(
            ListObject[Payout],
            await self._request_async(
                "get",
                "/v1/payouts",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self,
        params: "PayoutService.CreateParams",
        options: RequestOptions = {},
    ) -> Payout:
        """
        To send funds to your own bank account, create a new payout object. Your [Stripe balance](https://stripe.com/docs/api#balance) must cover the payout amount. If it doesn't, you receive an “Insufficient Funds” error.

        If your API key is in test mode, money won't actually be sent, though every other action occurs as if you're in live mode.

        If you create a manual payout on a Stripe account that uses multiple payment source types, you need to specify the source type balance that the payout draws from. The [balance object](https://stripe.com/docs/api#balance_object) details available and pending amounts by source type.
        """
        return cast(
            Payout,
            self._request(
                "post",
                "/v1/payouts",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        params: "PayoutService.CreateParams",
        options: RequestOptions = {},
    ) -> Payout:
        """
        To send funds to your own bank account, create a new payout object. Your [Stripe balance](https://stripe.com/docs/api#balance) must cover the payout amount. If it doesn't, you receive an “Insufficient Funds” error.

        If your API key is in test mode, money won't actually be sent, though every other action occurs as if you're in live mode.

        If you create a manual payout on a Stripe account that uses multiple payment source types, you need to specify the source type balance that the payout draws from. The [balance object](https://stripe.com/docs/api#balance_object) details available and pending amounts by source type.
        """
        return cast(
            Payout,
            await self._request_async(
                "post",
                "/v1/payouts",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        payout: str,
        params: "PayoutService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Payout:
        """
        Retrieves the details of an existing payout. Supply the unique payout ID from either a payout creation request or the payout list. Stripe returns the corresponding payout information.
        """
        return cast(
            Payout,
            self._request(
                "get",
                "/v1/payouts/{payout}".format(payout=sanitize_id(payout)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        payout: str,
        params: "PayoutService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Payout:
        """
        Retrieves the details of an existing payout. Supply the unique payout ID from either a payout creation request or the payout list. Stripe returns the corresponding payout information.
        """
        return cast(
            Payout,
            await self._request_async(
                "get",
                "/v1/payouts/{payout}".format(payout=sanitize_id(payout)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def update(
        self,
        payout: str,
        params: "PayoutService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Payout:
        """
        Updates the specified payout by setting the values of the parameters you pass. We don't change parameters that you don't provide. This request only accepts the metadata as arguments.
        """
        return cast(
            Payout,
            self._request(
                "post",
                "/v1/payouts/{payout}".format(payout=sanitize_id(payout)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def update_async(
        self,
        payout: str,
        params: "PayoutService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Payout:
        """
        Updates the specified payout by setting the values of the parameters you pass. We don't change parameters that you don't provide. This request only accepts the metadata as arguments.
        """
        return cast(
            Payout,
            await self._request_async(
                "post",
                "/v1/payouts/{payout}".format(payout=sanitize_id(payout)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def cancel(
        self,
        payout: str,
        params: "PayoutService.CancelParams" = {},
        options: RequestOptions = {},
    ) -> Payout:
        """
        You can cancel a previously created payout if its status is pending. Stripe refunds the funds to your available balance. You can't cancel automatic Stripe payouts.
        """
        return cast(
            Payout,
            self._request(
                "post",
                "/v1/payouts/{payout}/cancel".format(
                    payout=sanitize_id(payout),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def cancel_async(
        self,
        payout: str,
        params: "PayoutService.CancelParams" = {},
        options: RequestOptions = {},
    ) -> Payout:
        """
        You can cancel a previously created payout if its status is pending. Stripe refunds the funds to your available balance. You can't cancel automatic Stripe payouts.
        """
        return cast(
            Payout,
            await self._request_async(
                "post",
                "/v1/payouts/{payout}/cancel".format(
                    payout=sanitize_id(payout),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def reverse(
        self,
        payout: str,
        params: "PayoutService.ReverseParams" = {},
        options: RequestOptions = {},
    ) -> Payout:
        """
        Reverses a payout by debiting the destination bank account. At this time, you can only reverse payouts for connected accounts to US bank accounts. If the payout is manual and in the pending status, use /v1/payouts/:id/cancel instead.

        By requesting a reversal through /v1/payouts/:id/reverse, you confirm that the authorized signatory of the selected bank account authorizes the debit on the bank account and that no other authorization is required.
        """
        return cast(
            Payout,
            self._request(
                "post",
                "/v1/payouts/{payout}/reverse".format(
                    payout=sanitize_id(payout),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def reverse_async(
        self,
        payout: str,
        params: "PayoutService.ReverseParams" = {},
        options: RequestOptions = {},
    ) -> Payout:
        """
        Reverses a payout by debiting the destination bank account. At this time, you can only reverse payouts for connected accounts to US bank accounts. If the payout is manual and in the pending status, use /v1/payouts/:id/cancel instead.

        By requesting a reversal through /v1/payouts/:id/reverse, you confirm that the authorized signatory of the selected bank account authorizes the debit on the bank account and that no other authorization is required.
        """
        return cast(
            Payout,
            await self._request_async(
                "post",
                "/v1/payouts/{payout}/reverse".format(
                    payout=sanitize_id(payout),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

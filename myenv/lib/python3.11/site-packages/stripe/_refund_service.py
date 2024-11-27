# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._refund import Refund
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from typing import Dict, List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class RefundService(StripeService):
    class CancelParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class CreateParams(TypedDict):
        amount: NotRequired[int]
        charge: NotRequired[str]
        """
        The identifier of the charge to refund.
        """
        currency: NotRequired[str]
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        customer: NotRequired[str]
        """
        Customer whose customer balance to refund from.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        instructions_email: NotRequired[str]
        """
        For payment methods without native refund support (e.g., Konbini, PromptPay), use this email from the customer to receive refund instructions.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        origin: NotRequired[Literal["customer_balance"]]
        """
        Origin of the refund
        """
        payment_intent: NotRequired[str]
        """
        The identifier of the PaymentIntent to refund.
        """
        reason: NotRequired[
            Literal["duplicate", "fraudulent", "requested_by_customer"]
        ]
        """
        String indicating the reason for the refund. If set, possible values are `duplicate`, `fraudulent`, and `requested_by_customer`. If you believe the charge to be fraudulent, specifying `fraudulent` as the reason will add the associated card and email to your [block lists](https://stripe.com/docs/radar/lists), and will also help us improve our fraud detection algorithms.
        """
        refund_application_fee: NotRequired[bool]
        """
        Boolean indicating whether the application fee should be refunded when refunding this charge. If a full charge refund is given, the full application fee will be refunded. Otherwise, the application fee will be refunded in an amount proportional to the amount of the charge refunded. An application fee can be refunded only by the application that created the charge.
        """
        reverse_transfer: NotRequired[bool]
        """
        Boolean indicating whether the transfer should be reversed when refunding this charge. The transfer will be reversed proportionally to the amount being refunded (either the entire or partial amount).

        A transfer can be reversed only by the application that created the charge.
        """

    class ListParams(TypedDict):
        charge: NotRequired[str]
        """
        Only return refunds for the charge specified by this charge ID.
        """
        created: NotRequired["RefundService.ListParamsCreated|int"]
        """
        Only return refunds that were created during the given date interval.
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
        payment_intent: NotRequired[str]
        """
        Only return refunds for the PaymentIntent specified by this ID.
        """
        starting_after: NotRequired[str]
        """
        A cursor for use in pagination. `starting_after` is an object ID that defines your place in the list. For instance, if you make a list request and receive 100 objects, ending with `obj_foo`, your subsequent call can include `starting_after=obj_foo` in order to fetch the next page of the list.
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
        params: "RefundService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Refund]:
        """
        Returns a list of all refunds you created. We return the refunds in sorted order, with the most recent refunds appearing first. The 10 most recent refunds are always available by default on the Charge object.
        """
        return cast(
            ListObject[Refund],
            self._request(
                "get",
                "/v1/refunds",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "RefundService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Refund]:
        """
        Returns a list of all refunds you created. We return the refunds in sorted order, with the most recent refunds appearing first. The 10 most recent refunds are always available by default on the Charge object.
        """
        return cast(
            ListObject[Refund],
            await self._request_async(
                "get",
                "/v1/refunds",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self,
        params: "RefundService.CreateParams" = {},
        options: RequestOptions = {},
    ) -> Refund:
        """
        When you create a new refund, you must specify a Charge or a PaymentIntent object on which to create it.

        Creating a new refund will refund a charge that has previously been created but not yet refunded.
        Funds will be refunded to the credit or debit card that was originally charged.

        You can optionally refund only part of a charge.
        You can do so multiple times, until the entire charge has been refunded.

        Once entirely refunded, a charge can't be refunded again.
        This method will raise an error when called on an already-refunded charge,
        or when trying to refund more money than is left on a charge.
        """
        return cast(
            Refund,
            self._request(
                "post",
                "/v1/refunds",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        params: "RefundService.CreateParams" = {},
        options: RequestOptions = {},
    ) -> Refund:
        """
        When you create a new refund, you must specify a Charge or a PaymentIntent object on which to create it.

        Creating a new refund will refund a charge that has previously been created but not yet refunded.
        Funds will be refunded to the credit or debit card that was originally charged.

        You can optionally refund only part of a charge.
        You can do so multiple times, until the entire charge has been refunded.

        Once entirely refunded, a charge can't be refunded again.
        This method will raise an error when called on an already-refunded charge,
        or when trying to refund more money than is left on a charge.
        """
        return cast(
            Refund,
            await self._request_async(
                "post",
                "/v1/refunds",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        refund: str,
        params: "RefundService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Refund:
        """
        Retrieves the details of an existing refund.
        """
        return cast(
            Refund,
            self._request(
                "get",
                "/v1/refunds/{refund}".format(refund=sanitize_id(refund)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        refund: str,
        params: "RefundService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Refund:
        """
        Retrieves the details of an existing refund.
        """
        return cast(
            Refund,
            await self._request_async(
                "get",
                "/v1/refunds/{refund}".format(refund=sanitize_id(refund)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def update(
        self,
        refund: str,
        params: "RefundService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Refund:
        """
        Updates the refund that you specify by setting the values of the passed parameters. Any parameters that you don't provide remain unchanged.

        This request only accepts metadata as an argument.
        """
        return cast(
            Refund,
            self._request(
                "post",
                "/v1/refunds/{refund}".format(refund=sanitize_id(refund)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def update_async(
        self,
        refund: str,
        params: "RefundService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Refund:
        """
        Updates the refund that you specify by setting the values of the passed parameters. Any parameters that you don't provide remain unchanged.

        This request only accepts metadata as an argument.
        """
        return cast(
            Refund,
            await self._request_async(
                "post",
                "/v1/refunds/{refund}".format(refund=sanitize_id(refund)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def cancel(
        self,
        refund: str,
        params: "RefundService.CancelParams" = {},
        options: RequestOptions = {},
    ) -> Refund:
        """
        Cancels a refund with a status of requires_action.

        You can't cancel refunds in other states. Only refunds for payment methods that require customer action can enter the requires_action state.
        """
        return cast(
            Refund,
            self._request(
                "post",
                "/v1/refunds/{refund}/cancel".format(
                    refund=sanitize_id(refund),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def cancel_async(
        self,
        refund: str,
        params: "RefundService.CancelParams" = {},
        options: RequestOptions = {},
    ) -> Refund:
        """
        Cancels a refund with a status of requires_action.

        You can't cancel refunds in other states. Only refunds for payment methods that require customer action can enter the requires_action state.
        """
        return cast(
            Refund,
            await self._request_async(
                "post",
                "/v1/refunds/{refund}/cancel".format(
                    refund=sanitize_id(refund),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

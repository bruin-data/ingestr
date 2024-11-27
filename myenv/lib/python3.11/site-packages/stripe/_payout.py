# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._createable_api_resource import CreateableAPIResource
from stripe._expandable_field import ExpandableField
from stripe._list_object import ListObject
from stripe._listable_api_resource import ListableAPIResource
from stripe._request_options import RequestOptions
from stripe._updateable_api_resource import UpdateableAPIResource
from stripe._util import class_method_variant, sanitize_id
from typing import ClassVar, Dict, List, Optional, Union, cast, overload
from typing_extensions import (
    Literal,
    NotRequired,
    TypedDict,
    Unpack,
    TYPE_CHECKING,
)

if TYPE_CHECKING:
    from stripe._application_fee import ApplicationFee
    from stripe._balance_transaction import BalanceTransaction
    from stripe._bank_account import BankAccount
    from stripe._card import Card


class Payout(
    CreateableAPIResource["Payout"],
    ListableAPIResource["Payout"],
    UpdateableAPIResource["Payout"],
):
    """
    A `Payout` object is created when you receive funds from Stripe, or when you
    initiate a payout to either a bank account or debit card of a [connected
    Stripe account](https://stripe.com/docs/connect/bank-debit-card-payouts). You can retrieve individual payouts,
    and list all payouts. Payouts are made on [varying
    schedules](https://stripe.com/docs/connect/manage-payout-schedule), depending on your country and
    industry.

    Related guide: [Receiving payouts](https://stripe.com/docs/payouts)
    """

    OBJECT_NAME: ClassVar[Literal["payout"]] = "payout"

    class CancelParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class CreateParams(RequestOptions):
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

    class ListParams(RequestOptions):
        arrival_date: NotRequired["Payout.ListParamsArrivalDate|int"]
        """
        Only return payouts that are expected to arrive during the given date interval.
        """
        created: NotRequired["Payout.ListParamsCreated|int"]
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

    class ModifyParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """

    class RetrieveParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class ReverseParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """

    amount: int
    """
    The amount (in cents (or local equivalent)) that transfers to your bank account or debit card.
    """
    application_fee: Optional[ExpandableField["ApplicationFee"]]
    """
    The application fee (if any) for the payout. [See the Connect documentation](https://stripe.com/docs/connect/instant-payouts#monetization-and-fees) for details.
    """
    application_fee_amount: Optional[int]
    """
    The amount of the application fee (if any) requested for the payout. [See the Connect documentation](https://stripe.com/docs/connect/instant-payouts#monetization-and-fees) for details.
    """
    arrival_date: int
    """
    Date that you can expect the payout to arrive in the bank. This factors in delays to account for weekends or bank holidays.
    """
    automatic: bool
    """
    Returns `true` if the payout is created by an [automated payout schedule](https://stripe.com/docs/payouts#payout-schedule) and `false` if it's [requested manually](https://stripe.com/docs/payouts#manual-payouts).
    """
    balance_transaction: Optional[ExpandableField["BalanceTransaction"]]
    """
    ID of the balance transaction that describes the impact of this payout on your account balance.
    """
    created: int
    """
    Time at which the object was created. Measured in seconds since the Unix epoch.
    """
    currency: str
    """
    Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
    """
    description: Optional[str]
    """
    An arbitrary string attached to the object. Often useful for displaying to users.
    """
    destination: Optional[ExpandableField[Union["BankAccount", "Card"]]]
    """
    ID of the bank account or card the payout is sent to.
    """
    failure_balance_transaction: Optional[
        ExpandableField["BalanceTransaction"]
    ]
    """
    If the payout fails or cancels, this is the ID of the balance transaction that reverses the initial balance transaction and returns the funds from the failed payout back in your balance.
    """
    failure_code: Optional[str]
    """
    Error code that provides a reason for a payout failure, if available. View our [list of failure codes](https://stripe.com/docs/api#payout_failures).
    """
    failure_message: Optional[str]
    """
    Message that provides the reason for a payout failure, if available.
    """
    id: str
    """
    Unique identifier for the object.
    """
    livemode: bool
    """
    Has the value `true` if the object exists in live mode or the value `false` if the object exists in test mode.
    """
    metadata: Optional[Dict[str, str]]
    """
    Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format.
    """
    method: str
    """
    The method used to send this payout, which can be `standard` or `instant`. `instant` is supported for payouts to debit cards and bank accounts in certain countries. Learn more about [bank support for Instant Payouts](https://stripe.com/docs/payouts/instant-payouts-banks).
    """
    object: Literal["payout"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """
    original_payout: Optional[ExpandableField["Payout"]]
    """
    If the payout reverses another, this is the ID of the original payout.
    """
    reconciliation_status: Literal[
        "completed", "in_progress", "not_applicable"
    ]
    """
    If `completed`, you can use the [Balance Transactions API](https://stripe.com/docs/api/balance_transactions/list#balance_transaction_list-payout) to list all balance transactions that are paid out in this payout.
    """
    reversed_by: Optional[ExpandableField["Payout"]]
    """
    If the payout reverses, this is the ID of the payout that reverses this payout.
    """
    source_type: str
    """
    The source balance this payout came from, which can be one of the following: `card`, `fpx`, or `bank_account`.
    """
    statement_descriptor: Optional[str]
    """
    Extra information about a payout that displays on the user's bank statement.
    """
    status: str
    """
    Current status of the payout: `paid`, `pending`, `in_transit`, `canceled` or `failed`. A payout is `pending` until it's submitted to the bank, when it becomes `in_transit`. The status changes to `paid` if the transaction succeeds, or to `failed` or `canceled` (within 5 business days). Some payouts that fail might initially show as `paid`, then change to `failed`.
    """
    type: Literal["bank_account", "card"]
    """
    Can be `bank_account` or `card`.
    """

    @classmethod
    def _cls_cancel(
        cls, payout: str, **params: Unpack["Payout.CancelParams"]
    ) -> "Payout":
        """
        You can cancel a previously created payout if its status is pending. Stripe refunds the funds to your available balance. You can't cancel automatic Stripe payouts.
        """
        return cast(
            "Payout",
            cls._static_request(
                "post",
                "/v1/payouts/{payout}/cancel".format(
                    payout=sanitize_id(payout)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    def cancel(
        payout: str, **params: Unpack["Payout.CancelParams"]
    ) -> "Payout":
        """
        You can cancel a previously created payout if its status is pending. Stripe refunds the funds to your available balance. You can't cancel automatic Stripe payouts.
        """
        ...

    @overload
    def cancel(self, **params: Unpack["Payout.CancelParams"]) -> "Payout":
        """
        You can cancel a previously created payout if its status is pending. Stripe refunds the funds to your available balance. You can't cancel automatic Stripe payouts.
        """
        ...

    @class_method_variant("_cls_cancel")
    def cancel(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Payout.CancelParams"]
    ) -> "Payout":
        """
        You can cancel a previously created payout if its status is pending. Stripe refunds the funds to your available balance. You can't cancel automatic Stripe payouts.
        """
        return cast(
            "Payout",
            self._request(
                "post",
                "/v1/payouts/{payout}/cancel".format(
                    payout=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    async def _cls_cancel_async(
        cls, payout: str, **params: Unpack["Payout.CancelParams"]
    ) -> "Payout":
        """
        You can cancel a previously created payout if its status is pending. Stripe refunds the funds to your available balance. You can't cancel automatic Stripe payouts.
        """
        return cast(
            "Payout",
            await cls._static_request_async(
                "post",
                "/v1/payouts/{payout}/cancel".format(
                    payout=sanitize_id(payout)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    async def cancel_async(
        payout: str, **params: Unpack["Payout.CancelParams"]
    ) -> "Payout":
        """
        You can cancel a previously created payout if its status is pending. Stripe refunds the funds to your available balance. You can't cancel automatic Stripe payouts.
        """
        ...

    @overload
    async def cancel_async(
        self, **params: Unpack["Payout.CancelParams"]
    ) -> "Payout":
        """
        You can cancel a previously created payout if its status is pending. Stripe refunds the funds to your available balance. You can't cancel automatic Stripe payouts.
        """
        ...

    @class_method_variant("_cls_cancel_async")
    async def cancel_async(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Payout.CancelParams"]
    ) -> "Payout":
        """
        You can cancel a previously created payout if its status is pending. Stripe refunds the funds to your available balance. You can't cancel automatic Stripe payouts.
        """
        return cast(
            "Payout",
            await self._request_async(
                "post",
                "/v1/payouts/{payout}/cancel".format(
                    payout=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    def create(cls, **params: Unpack["Payout.CreateParams"]) -> "Payout":
        """
        To send funds to your own bank account, create a new payout object. Your [Stripe balance](https://stripe.com/docs/api#balance) must cover the payout amount. If it doesn't, you receive an “Insufficient Funds” error.

        If your API key is in test mode, money won't actually be sent, though every other action occurs as if you're in live mode.

        If you create a manual payout on a Stripe account that uses multiple payment source types, you need to specify the source type balance that the payout draws from. The [balance object](https://stripe.com/docs/api#balance_object) details available and pending amounts by source type.
        """
        return cast(
            "Payout",
            cls._static_request(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    async def create_async(
        cls, **params: Unpack["Payout.CreateParams"]
    ) -> "Payout":
        """
        To send funds to your own bank account, create a new payout object. Your [Stripe balance](https://stripe.com/docs/api#balance) must cover the payout amount. If it doesn't, you receive an “Insufficient Funds” error.

        If your API key is in test mode, money won't actually be sent, though every other action occurs as if you're in live mode.

        If you create a manual payout on a Stripe account that uses multiple payment source types, you need to specify the source type balance that the payout draws from. The [balance object](https://stripe.com/docs/api#balance_object) details available and pending amounts by source type.
        """
        return cast(
            "Payout",
            await cls._static_request_async(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    def list(
        cls, **params: Unpack["Payout.ListParams"]
    ) -> ListObject["Payout"]:
        """
        Returns a list of existing payouts sent to third-party bank accounts or payouts that Stripe sent to you. The payouts return in sorted order, with the most recently created payouts appearing first.
        """
        result = cls._static_request(
            "get",
            cls.class_url(),
            params=params,
        )
        if not isinstance(result, ListObject):
            raise TypeError(
                "Expected list object from API, got %s"
                % (type(result).__name__)
            )

        return result

    @classmethod
    async def list_async(
        cls, **params: Unpack["Payout.ListParams"]
    ) -> ListObject["Payout"]:
        """
        Returns a list of existing payouts sent to third-party bank accounts or payouts that Stripe sent to you. The payouts return in sorted order, with the most recently created payouts appearing first.
        """
        result = await cls._static_request_async(
            "get",
            cls.class_url(),
            params=params,
        )
        if not isinstance(result, ListObject):
            raise TypeError(
                "Expected list object from API, got %s"
                % (type(result).__name__)
            )

        return result

    @classmethod
    def modify(
        cls, id: str, **params: Unpack["Payout.ModifyParams"]
    ) -> "Payout":
        """
        Updates the specified payout by setting the values of the parameters you pass. We don't change parameters that you don't provide. This request only accepts the metadata as arguments.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(id))
        return cast(
            "Payout",
            cls._static_request(
                "post",
                url,
                params=params,
            ),
        )

    @classmethod
    async def modify_async(
        cls, id: str, **params: Unpack["Payout.ModifyParams"]
    ) -> "Payout":
        """
        Updates the specified payout by setting the values of the parameters you pass. We don't change parameters that you don't provide. This request only accepts the metadata as arguments.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(id))
        return cast(
            "Payout",
            await cls._static_request_async(
                "post",
                url,
                params=params,
            ),
        )

    @classmethod
    def retrieve(
        cls, id: str, **params: Unpack["Payout.RetrieveParams"]
    ) -> "Payout":
        """
        Retrieves the details of an existing payout. Supply the unique payout ID from either a payout creation request or the payout list. Stripe returns the corresponding payout information.
        """
        instance = cls(id, **params)
        instance.refresh()
        return instance

    @classmethod
    async def retrieve_async(
        cls, id: str, **params: Unpack["Payout.RetrieveParams"]
    ) -> "Payout":
        """
        Retrieves the details of an existing payout. Supply the unique payout ID from either a payout creation request or the payout list. Stripe returns the corresponding payout information.
        """
        instance = cls(id, **params)
        await instance.refresh_async()
        return instance

    @classmethod
    def _cls_reverse(
        cls, payout: str, **params: Unpack["Payout.ReverseParams"]
    ) -> "Payout":
        """
        Reverses a payout by debiting the destination bank account. At this time, you can only reverse payouts for connected accounts to US bank accounts. If the payout is manual and in the pending status, use /v1/payouts/:id/cancel instead.

        By requesting a reversal through /v1/payouts/:id/reverse, you confirm that the authorized signatory of the selected bank account authorizes the debit on the bank account and that no other authorization is required.
        """
        return cast(
            "Payout",
            cls._static_request(
                "post",
                "/v1/payouts/{payout}/reverse".format(
                    payout=sanitize_id(payout)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    def reverse(
        payout: str, **params: Unpack["Payout.ReverseParams"]
    ) -> "Payout":
        """
        Reverses a payout by debiting the destination bank account. At this time, you can only reverse payouts for connected accounts to US bank accounts. If the payout is manual and in the pending status, use /v1/payouts/:id/cancel instead.

        By requesting a reversal through /v1/payouts/:id/reverse, you confirm that the authorized signatory of the selected bank account authorizes the debit on the bank account and that no other authorization is required.
        """
        ...

    @overload
    def reverse(self, **params: Unpack["Payout.ReverseParams"]) -> "Payout":
        """
        Reverses a payout by debiting the destination bank account. At this time, you can only reverse payouts for connected accounts to US bank accounts. If the payout is manual and in the pending status, use /v1/payouts/:id/cancel instead.

        By requesting a reversal through /v1/payouts/:id/reverse, you confirm that the authorized signatory of the selected bank account authorizes the debit on the bank account and that no other authorization is required.
        """
        ...

    @class_method_variant("_cls_reverse")
    def reverse(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Payout.ReverseParams"]
    ) -> "Payout":
        """
        Reverses a payout by debiting the destination bank account. At this time, you can only reverse payouts for connected accounts to US bank accounts. If the payout is manual and in the pending status, use /v1/payouts/:id/cancel instead.

        By requesting a reversal through /v1/payouts/:id/reverse, you confirm that the authorized signatory of the selected bank account authorizes the debit on the bank account and that no other authorization is required.
        """
        return cast(
            "Payout",
            self._request(
                "post",
                "/v1/payouts/{payout}/reverse".format(
                    payout=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    async def _cls_reverse_async(
        cls, payout: str, **params: Unpack["Payout.ReverseParams"]
    ) -> "Payout":
        """
        Reverses a payout by debiting the destination bank account. At this time, you can only reverse payouts for connected accounts to US bank accounts. If the payout is manual and in the pending status, use /v1/payouts/:id/cancel instead.

        By requesting a reversal through /v1/payouts/:id/reverse, you confirm that the authorized signatory of the selected bank account authorizes the debit on the bank account and that no other authorization is required.
        """
        return cast(
            "Payout",
            await cls._static_request_async(
                "post",
                "/v1/payouts/{payout}/reverse".format(
                    payout=sanitize_id(payout)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    async def reverse_async(
        payout: str, **params: Unpack["Payout.ReverseParams"]
    ) -> "Payout":
        """
        Reverses a payout by debiting the destination bank account. At this time, you can only reverse payouts for connected accounts to US bank accounts. If the payout is manual and in the pending status, use /v1/payouts/:id/cancel instead.

        By requesting a reversal through /v1/payouts/:id/reverse, you confirm that the authorized signatory of the selected bank account authorizes the debit on the bank account and that no other authorization is required.
        """
        ...

    @overload
    async def reverse_async(
        self, **params: Unpack["Payout.ReverseParams"]
    ) -> "Payout":
        """
        Reverses a payout by debiting the destination bank account. At this time, you can only reverse payouts for connected accounts to US bank accounts. If the payout is manual and in the pending status, use /v1/payouts/:id/cancel instead.

        By requesting a reversal through /v1/payouts/:id/reverse, you confirm that the authorized signatory of the selected bank account authorizes the debit on the bank account and that no other authorization is required.
        """
        ...

    @class_method_variant("_cls_reverse_async")
    async def reverse_async(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Payout.ReverseParams"]
    ) -> "Payout":
        """
        Reverses a payout by debiting the destination bank account. At this time, you can only reverse payouts for connected accounts to US bank accounts. If the payout is manual and in the pending status, use /v1/payouts/:id/cancel instead.

        By requesting a reversal through /v1/payouts/:id/reverse, you confirm that the authorized signatory of the selected bank account authorizes the debit on the bank account and that no other authorization is required.
        """
        return cast(
            "Payout",
            await self._request_async(
                "post",
                "/v1/payouts/{payout}/reverse".format(
                    payout=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

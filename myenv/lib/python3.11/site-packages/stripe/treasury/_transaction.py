# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._listable_api_resource import ListableAPIResource
from stripe._request_options import RequestOptions
from stripe._stripe_object import StripeObject
from typing import ClassVar, List, Optional
from typing_extensions import (
    Literal,
    NotRequired,
    TypedDict,
    Unpack,
    TYPE_CHECKING,
)

if TYPE_CHECKING:
    from stripe.issuing._authorization import Authorization
    from stripe.treasury._credit_reversal import CreditReversal
    from stripe.treasury._debit_reversal import DebitReversal
    from stripe.treasury._inbound_transfer import InboundTransfer
    from stripe.treasury._outbound_payment import OutboundPayment
    from stripe.treasury._outbound_transfer import OutboundTransfer
    from stripe.treasury._received_credit import ReceivedCredit
    from stripe.treasury._received_debit import ReceivedDebit
    from stripe.treasury._transaction_entry import TransactionEntry


class Transaction(ListableAPIResource["Transaction"]):
    """
    Transactions represent changes to a [FinancialAccount's](https://stripe.com/docs/api#financial_accounts) balance.
    """

    OBJECT_NAME: ClassVar[Literal["treasury.transaction"]] = (
        "treasury.transaction"
    )

    class BalanceImpact(StripeObject):
        cash: int
        """
        The change made to funds the user can spend right now.
        """
        inbound_pending: int
        """
        The change made to funds that are not spendable yet, but will become available at a later time.
        """
        outbound_pending: int
        """
        The change made to funds in the account, but not spendable because they are being held for pending outbound flows.
        """

    class FlowDetails(StripeObject):
        credit_reversal: Optional["CreditReversal"]
        """
        You can reverse some [ReceivedCredits](https://stripe.com/docs/api#received_credits) depending on their network and source flow. Reversing a ReceivedCredit leads to the creation of a new object known as a CreditReversal.
        """
        debit_reversal: Optional["DebitReversal"]
        """
        You can reverse some [ReceivedDebits](https://stripe.com/docs/api#received_debits) depending on their network and source flow. Reversing a ReceivedDebit leads to the creation of a new object known as a DebitReversal.
        """
        inbound_transfer: Optional["InboundTransfer"]
        """
        Use [InboundTransfers](https://stripe.com/docs/treasury/moving-money/financial-accounts/into/inbound-transfers) to add funds to your [FinancialAccount](https://stripe.com/docs/api#financial_accounts) via a PaymentMethod that is owned by you. The funds will be transferred via an ACH debit.
        """
        issuing_authorization: Optional["Authorization"]
        """
        When an [issued card](https://stripe.com/docs/issuing) is used to make a purchase, an Issuing `Authorization`
        object is created. [Authorizations](https://stripe.com/docs/issuing/purchases/authorizations) must be approved for the
        purchase to be completed successfully.

        Related guide: [Issued card authorizations](https://stripe.com/docs/issuing/purchases/authorizations)
        """
        outbound_payment: Optional["OutboundPayment"]
        """
        Use OutboundPayments to send funds to another party's external bank account or [FinancialAccount](https://stripe.com/docs/api#financial_accounts). To send money to an account belonging to the same user, use an [OutboundTransfer](https://stripe.com/docs/api#outbound_transfers).

        Simulate OutboundPayment state changes with the `/v1/test_helpers/treasury/outbound_payments` endpoints. These methods can only be called on test mode objects.
        """
        outbound_transfer: Optional["OutboundTransfer"]
        """
        Use OutboundTransfers to transfer funds from a [FinancialAccount](https://stripe.com/docs/api#financial_accounts) to a PaymentMethod belonging to the same entity. To send funds to a different party, use [OutboundPayments](https://stripe.com/docs/api#outbound_payments) instead. You can send funds over ACH rails or through a domestic wire transfer to a user's own external bank account.

        Simulate OutboundTransfer state changes with the `/v1/test_helpers/treasury/outbound_transfers` endpoints. These methods can only be called on test mode objects.
        """
        received_credit: Optional["ReceivedCredit"]
        """
        ReceivedCredits represent funds sent to a [FinancialAccount](https://stripe.com/docs/api#financial_accounts) (for example, via ACH or wire). These money movements are not initiated from the FinancialAccount.
        """
        received_debit: Optional["ReceivedDebit"]
        """
        ReceivedDebits represent funds pulled from a [FinancialAccount](https://stripe.com/docs/api#financial_accounts). These are not initiated from the FinancialAccount.
        """
        type: Literal[
            "credit_reversal",
            "debit_reversal",
            "inbound_transfer",
            "issuing_authorization",
            "other",
            "outbound_payment",
            "outbound_transfer",
            "received_credit",
            "received_debit",
        ]
        """
        Type of the flow that created the Transaction. Set to the same value as `flow_type`.
        """

    class StatusTransitions(StripeObject):
        posted_at: Optional[int]
        """
        Timestamp describing when the Transaction changed status to `posted`.
        """
        void_at: Optional[int]
        """
        Timestamp describing when the Transaction changed status to `void`.
        """

    class ListParams(RequestOptions):
        created: NotRequired["Transaction.ListParamsCreated|int"]
        """
        Only return Transactions that were created during the given date interval.
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
        order_by: NotRequired[Literal["created", "posted_at"]]
        """
        The results are in reverse chronological order by `created` or `posted_at`. The default is `created`.
        """
        starting_after: NotRequired[str]
        """
        A cursor for use in pagination. `starting_after` is an object ID that defines your place in the list. For instance, if you make a list request and receive 100 objects, ending with `obj_foo`, your subsequent call can include `starting_after=obj_foo` in order to fetch the next page of the list.
        """
        status: NotRequired[Literal["open", "posted", "void"]]
        """
        Only return Transactions that have the given status: `open`, `posted`, or `void`.
        """
        status_transitions: NotRequired[
            "Transaction.ListParamsStatusTransitions"
        ]
        """
        A filter for the `status_transitions.posted_at` timestamp. When using this filter, `status=posted` and `order_by=posted_at` must also be specified.
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

    class ListParamsStatusTransitions(TypedDict):
        posted_at: NotRequired[
            "Transaction.ListParamsStatusTransitionsPostedAt|int"
        ]
        """
        Returns Transactions with `posted_at` within the specified range.
        """

    class ListParamsStatusTransitionsPostedAt(TypedDict):
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

    class RetrieveParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    amount: int
    """
    Amount (in cents) transferred.
    """
    balance_impact: BalanceImpact
    """
    Change to a FinancialAccount's balance
    """
    created: int
    """
    Time at which the object was created. Measured in seconds since the Unix epoch.
    """
    currency: str
    """
    Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
    """
    description: str
    """
    An arbitrary string attached to the object. Often useful for displaying to users.
    """
    entries: Optional[ListObject["TransactionEntry"]]
    """
    A list of TransactionEntries that are part of this Transaction. This cannot be expanded in any list endpoints.
    """
    financial_account: str
    """
    The FinancialAccount associated with this object.
    """
    flow: Optional[str]
    """
    ID of the flow that created the Transaction.
    """
    flow_details: Optional[FlowDetails]
    """
    Details of the flow that created the Transaction.
    """
    flow_type: Literal[
        "credit_reversal",
        "debit_reversal",
        "inbound_transfer",
        "issuing_authorization",
        "other",
        "outbound_payment",
        "outbound_transfer",
        "received_credit",
        "received_debit",
    ]
    """
    Type of the flow that created the Transaction.
    """
    id: str
    """
    Unique identifier for the object.
    """
    livemode: bool
    """
    Has the value `true` if the object exists in live mode or the value `false` if the object exists in test mode.
    """
    object: Literal["treasury.transaction"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """
    status: Literal["open", "posted", "void"]
    """
    Status of the Transaction.
    """
    status_transitions: StatusTransitions

    @classmethod
    def list(
        cls, **params: Unpack["Transaction.ListParams"]
    ) -> ListObject["Transaction"]:
        """
        Retrieves a list of Transaction objects.
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
        cls, **params: Unpack["Transaction.ListParams"]
    ) -> ListObject["Transaction"]:
        """
        Retrieves a list of Transaction objects.
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
    def retrieve(
        cls, id: str, **params: Unpack["Transaction.RetrieveParams"]
    ) -> "Transaction":
        """
        Retrieves the details of an existing Transaction.
        """
        instance = cls(id, **params)
        instance.refresh()
        return instance

    @classmethod
    async def retrieve_async(
        cls, id: str, **params: Unpack["Transaction.RetrieveParams"]
    ) -> "Transaction":
        """
        Retrieves the details of an existing Transaction.
        """
        instance = cls(id, **params)
        await instance.refresh_async()
        return instance

    _inner_class_types = {
        "balance_impact": BalanceImpact,
        "flow_details": FlowDetails,
        "status_transitions": StatusTransitions,
    }

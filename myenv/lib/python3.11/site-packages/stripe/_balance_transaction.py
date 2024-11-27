# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._expandable_field import ExpandableField
from stripe._list_object import ListObject
from stripe._listable_api_resource import ListableAPIResource
from stripe._request_options import RequestOptions
from stripe._stripe_object import StripeObject
from typing import ClassVar, List, Optional, Union
from typing_extensions import (
    Literal,
    NotRequired,
    TypedDict,
    Unpack,
    TYPE_CHECKING,
)

if TYPE_CHECKING:
    from stripe._application_fee import ApplicationFee
    from stripe._application_fee_refund import ApplicationFeeRefund
    from stripe._charge import Charge
    from stripe._connect_collection_transfer import ConnectCollectionTransfer
    from stripe._customer_cash_balance_transaction import (
        CustomerCashBalanceTransaction,
    )
    from stripe._dispute import Dispute as DisputeResource
    from stripe._payout import Payout
    from stripe._refund import Refund
    from stripe._reserve_transaction import ReserveTransaction
    from stripe._reversal import Reversal
    from stripe._tax_deducted_at_source import TaxDeductedAtSource
    from stripe._topup import Topup
    from stripe._transfer import Transfer
    from stripe.issuing._authorization import Authorization
    from stripe.issuing._dispute import Dispute as IssuingDisputeResource
    from stripe.issuing._transaction import Transaction


class BalanceTransaction(ListableAPIResource["BalanceTransaction"]):
    """
    Balance transactions represent funds moving through your Stripe account.
    Stripe creates them for every type of transaction that enters or leaves your Stripe account balance.

    Related guide: [Balance transaction types](https://stripe.com/docs/reports/balance-transaction-types)
    """

    OBJECT_NAME: ClassVar[Literal["balance_transaction"]] = (
        "balance_transaction"
    )

    class FeeDetail(StripeObject):
        amount: int
        """
        Amount of the fee, in cents.
        """
        application: Optional[str]
        """
        ID of the Connect application that earned the fee.
        """
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        description: Optional[str]
        """
        An arbitrary string attached to the object. Often useful for displaying to users.
        """
        type: str
        """
        Type of the fee, one of: `application_fee`, `payment_method_passthrough_fee`, `stripe_fee` or `tax`.
        """

    class ListParams(RequestOptions):
        created: NotRequired["BalanceTransaction.ListParamsCreated|int"]
        """
        Only return transactions that were created during the given date interval.
        """
        currency: NotRequired[str]
        """
        Only return transactions in a certain currency. Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
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
        payout: NotRequired[str]
        """
        For automatic Stripe payouts only, only returns transactions that were paid out on the specified payout ID.
        """
        source: NotRequired[str]
        """
        Only returns the original transaction.
        """
        starting_after: NotRequired[str]
        """
        A cursor for use in pagination. `starting_after` is an object ID that defines your place in the list. For instance, if you make a list request and receive 100 objects, ending with `obj_foo`, your subsequent call can include `starting_after=obj_foo` in order to fetch the next page of the list.
        """
        type: NotRequired[str]
        """
        Only returns transactions of the given type. One of: `adjustment`, `advance`, `advance_funding`, `anticipation_repayment`, `application_fee`, `application_fee_refund`, `charge`, `climate_order_purchase`, `climate_order_refund`, `connect_collection_transfer`, `contribution`, `issuing_authorization_hold`, `issuing_authorization_release`, `issuing_dispute`, `issuing_transaction`, `obligation_outbound`, `obligation_reversal_inbound`, `payment`, `payment_failure_refund`, `payment_network_reserve_hold`, `payment_network_reserve_release`, `payment_refund`, `payment_reversal`, `payment_unreconciled`, `payout`, `payout_cancel`, `payout_failure`, `refund`, `refund_failure`, `reserve_transaction`, `reserved_funds`, `stripe_fee`, `stripe_fx_fee`, `tax_fee`, `topup`, `topup_reversal`, `transfer`, `transfer_cancel`, `transfer_failure`, or `transfer_refund`.
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

    class RetrieveParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    amount: int
    """
    Gross amount of this transaction (in cents (or local equivalent)). A positive value represents funds charged to another party, and a negative value represents funds sent to another party.
    """
    available_on: int
    """
    The date that the transaction's net funds become available in the Stripe balance.
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
    exchange_rate: Optional[float]
    """
    If applicable, this transaction uses an exchange rate. If money converts from currency A to currency B, then the `amount` in currency A, multipled by the `exchange_rate`, equals the `amount` in currency B. For example, if you charge a customer 10.00 EUR, the PaymentIntent's `amount` is `1000` and `currency` is `eur`. If this converts to 12.34 USD in your Stripe account, the BalanceTransaction's `amount` is `1234`, its `currency` is `usd`, and the `exchange_rate` is `1.234`.
    """
    fee: int
    """
    Fees (in cents (or local equivalent)) paid for this transaction. Represented as a positive integer when assessed.
    """
    fee_details: List[FeeDetail]
    """
    Detailed breakdown of fees (in cents (or local equivalent)) paid for this transaction.
    """
    id: str
    """
    Unique identifier for the object.
    """
    net: int
    """
    Net impact to a Stripe balance (in cents (or local equivalent)). A positive value represents incrementing a Stripe balance, and a negative value decrementing a Stripe balance. You can calculate the net impact of a transaction on a balance by `amount` - `fee`
    """
    object: Literal["balance_transaction"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """
    reporting_category: str
    """
    Learn more about how [reporting categories](https://stripe.com/docs/reports/reporting-categories) can help you understand balance transactions from an accounting perspective.
    """
    source: Optional[
        ExpandableField[
            Union[
                "ApplicationFee",
                "Charge",
                "ConnectCollectionTransfer",
                "CustomerCashBalanceTransaction",
                "DisputeResource",
                "ApplicationFeeRefund",
                "Authorization",
                "IssuingDisputeResource",
                "Transaction",
                "Payout",
                "Refund",
                "ReserveTransaction",
                "TaxDeductedAtSource",
                "Topup",
                "Transfer",
                "Reversal",
            ]
        ]
    ]
    """
    This transaction relates to the Stripe object.
    """
    status: str
    """
    The transaction's net funds status in the Stripe balance, which are either `available` or `pending`.
    """
    type: Literal[
        "adjustment",
        "advance",
        "advance_funding",
        "anticipation_repayment",
        "application_fee",
        "application_fee_refund",
        "charge",
        "climate_order_purchase",
        "climate_order_refund",
        "connect_collection_transfer",
        "contribution",
        "issuing_authorization_hold",
        "issuing_authorization_release",
        "issuing_dispute",
        "issuing_transaction",
        "obligation_outbound",
        "obligation_reversal_inbound",
        "payment",
        "payment_failure_refund",
        "payment_network_reserve_hold",
        "payment_network_reserve_release",
        "payment_refund",
        "payment_reversal",
        "payment_unreconciled",
        "payout",
        "payout_cancel",
        "payout_failure",
        "refund",
        "refund_failure",
        "reserve_transaction",
        "reserved_funds",
        "stripe_fee",
        "stripe_fx_fee",
        "tax_fee",
        "topup",
        "topup_reversal",
        "transfer",
        "transfer_cancel",
        "transfer_failure",
        "transfer_refund",
    ]
    """
    Transaction type: `adjustment`, `advance`, `advance_funding`, `anticipation_repayment`, `application_fee`, `application_fee_refund`, `charge`, `climate_order_purchase`, `climate_order_refund`, `connect_collection_transfer`, `contribution`, `issuing_authorization_hold`, `issuing_authorization_release`, `issuing_dispute`, `issuing_transaction`, `obligation_outbound`, `obligation_reversal_inbound`, `payment`, `payment_failure_refund`, `payment_network_reserve_hold`, `payment_network_reserve_release`, `payment_refund`, `payment_reversal`, `payment_unreconciled`, `payout`, `payout_cancel`, `payout_failure`, `refund`, `refund_failure`, `reserve_transaction`, `reserved_funds`, `stripe_fee`, `stripe_fx_fee`, `tax_fee`, `topup`, `topup_reversal`, `transfer`, `transfer_cancel`, `transfer_failure`, or `transfer_refund`. Learn more about [balance transaction types and what they represent](https://stripe.com/docs/reports/balance-transaction-types). To classify transactions for accounting purposes, consider `reporting_category` instead.
    """

    @classmethod
    def list(
        cls, **params: Unpack["BalanceTransaction.ListParams"]
    ) -> ListObject["BalanceTransaction"]:
        """
        Returns a list of transactions that have contributed to the Stripe account balance (e.g., charges, transfers, and so forth). The transactions are returned in sorted order, with the most recent transactions appearing first.

        Note that this endpoint was previously called “Balance history” and used the path /v1/balance/history.
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
        cls, **params: Unpack["BalanceTransaction.ListParams"]
    ) -> ListObject["BalanceTransaction"]:
        """
        Returns a list of transactions that have contributed to the Stripe account balance (e.g., charges, transfers, and so forth). The transactions are returned in sorted order, with the most recent transactions appearing first.

        Note that this endpoint was previously called “Balance history” and used the path /v1/balance/history.
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
        cls, id: str, **params: Unpack["BalanceTransaction.RetrieveParams"]
    ) -> "BalanceTransaction":
        """
        Retrieves the balance transaction with the given ID.

        Note that this endpoint previously used the path /v1/balance/history/:id.
        """
        instance = cls(id, **params)
        instance.refresh()
        return instance

    @classmethod
    async def retrieve_async(
        cls, id: str, **params: Unpack["BalanceTransaction.RetrieveParams"]
    ) -> "BalanceTransaction":
        """
        Retrieves the balance transaction with the given ID.

        Note that this endpoint previously used the path /v1/balance/history/:id.
        """
        instance = cls(id, **params)
        await instance.refresh_async()
        return instance

    _inner_class_types = {"fee_details": FeeDetail}

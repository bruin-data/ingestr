# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._expandable_field import ExpandableField
from stripe._list_object import ListObject
from stripe._listable_api_resource import ListableAPIResource
from stripe._request_options import RequestOptions
from stripe._stripe_object import StripeObject
from stripe._test_helpers import APIResourceTestHelpers
from typing import ClassVar, List, Optional, cast
from typing_extensions import (
    Literal,
    NotRequired,
    Type,
    TypedDict,
    Unpack,
    TYPE_CHECKING,
)

if TYPE_CHECKING:
    from stripe.treasury._transaction import Transaction


class ReceivedDebit(ListableAPIResource["ReceivedDebit"]):
    """
    ReceivedDebits represent funds pulled from a [FinancialAccount](https://stripe.com/docs/api#financial_accounts). These are not initiated from the FinancialAccount.
    """

    OBJECT_NAME: ClassVar[Literal["treasury.received_debit"]] = (
        "treasury.received_debit"
    )

    class InitiatingPaymentMethodDetails(StripeObject):
        class BillingDetails(StripeObject):
            class Address(StripeObject):
                city: Optional[str]
                """
                City, district, suburb, town, or village.
                """
                country: Optional[str]
                """
                Two-letter country code ([ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)).
                """
                line1: Optional[str]
                """
                Address line 1 (e.g., street, PO Box, or company name).
                """
                line2: Optional[str]
                """
                Address line 2 (e.g., apartment, suite, unit, or building).
                """
                postal_code: Optional[str]
                """
                ZIP or postal code.
                """
                state: Optional[str]
                """
                State, county, province, or region.
                """

            address: Address
            email: Optional[str]
            """
            Email address.
            """
            name: Optional[str]
            """
            Full name.
            """
            _inner_class_types = {"address": Address}

        class FinancialAccount(StripeObject):
            id: str
            """
            The FinancialAccount ID.
            """
            network: Literal["stripe"]
            """
            The rails the ReceivedCredit was sent over. A FinancialAccount can only send funds over `stripe`.
            """

        class UsBankAccount(StripeObject):
            bank_name: Optional[str]
            """
            Bank name.
            """
            last4: Optional[str]
            """
            The last four digits of the bank account number.
            """
            routing_number: Optional[str]
            """
            The routing number for the bank account.
            """

        balance: Optional[Literal["payments"]]
        """
        Set when `type` is `balance`.
        """
        billing_details: BillingDetails
        financial_account: Optional[FinancialAccount]
        issuing_card: Optional[str]
        """
        Set when `type` is `issuing_card`. This is an [Issuing Card](https://stripe.com/docs/api#issuing_cards) ID.
        """
        type: Literal[
            "balance",
            "financial_account",
            "issuing_card",
            "stripe",
            "us_bank_account",
        ]
        """
        Polymorphic type matching the originating money movement's source. This can be an external account, a Stripe balance, or a FinancialAccount.
        """
        us_bank_account: Optional[UsBankAccount]
        _inner_class_types = {
            "billing_details": BillingDetails,
            "financial_account": FinancialAccount,
            "us_bank_account": UsBankAccount,
        }

    class LinkedFlows(StripeObject):
        debit_reversal: Optional[str]
        """
        The DebitReversal created as a result of this ReceivedDebit being reversed.
        """
        inbound_transfer: Optional[str]
        """
        Set if the ReceivedDebit is associated with an InboundTransfer's return of funds.
        """
        issuing_authorization: Optional[str]
        """
        Set if the ReceivedDebit was created due to an [Issuing Authorization](https://stripe.com/docs/api#issuing_authorizations) object.
        """
        issuing_transaction: Optional[str]
        """
        Set if the ReceivedDebit is also viewable as an [Issuing Dispute](https://stripe.com/docs/api#issuing_disputes) object.
        """
        payout: Optional[str]
        """
        Set if the ReceivedDebit was created due to a [Payout](https://stripe.com/docs/api#payouts) object.
        """

    class ReversalDetails(StripeObject):
        deadline: Optional[int]
        """
        Time before which a ReceivedDebit can be reversed.
        """
        restricted_reason: Optional[
            Literal[
                "already_reversed",
                "deadline_passed",
                "network_restricted",
                "other",
                "source_flow_restricted",
            ]
        ]
        """
        Set if a ReceivedDebit can't be reversed.
        """

    class CreateParams(RequestOptions):
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
            "ReceivedDebit.CreateParamsInitiatingPaymentMethodDetails"
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
            "ReceivedDebit.CreateParamsInitiatingPaymentMethodDetailsUsBankAccount"
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

    class ListParams(RequestOptions):
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
        The FinancialAccount that funds were pulled from.
        """
        limit: NotRequired[int]
        """
        A limit on the number of objects to be returned. Limit can range between 1 and 100, and the default is 10.
        """
        starting_after: NotRequired[str]
        """
        A cursor for use in pagination. `starting_after` is an object ID that defines your place in the list. For instance, if you make a list request and receive 100 objects, ending with `obj_foo`, your subsequent call can include `starting_after=obj_foo` in order to fetch the next page of the list.
        """
        status: NotRequired[Literal["failed", "succeeded"]]
        """
        Only return ReceivedDebits that have the given status: `succeeded` or `failed`.
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
    failure_code: Optional[
        Literal[
            "account_closed", "account_frozen", "insufficient_funds", "other"
        ]
    ]
    """
    Reason for the failure. A ReceivedDebit might fail because the FinancialAccount doesn't have sufficient funds, is closed, or is frozen.
    """
    financial_account: Optional[str]
    """
    The FinancialAccount that funds were pulled from.
    """
    hosted_regulatory_receipt_url: Optional[str]
    """
    A [hosted transaction receipt](https://stripe.com/docs/treasury/moving-money/regulatory-receipts) URL that is provided when money movement is considered regulated under Stripe's money transmission licenses.
    """
    id: str
    """
    Unique identifier for the object.
    """
    initiating_payment_method_details: Optional[InitiatingPaymentMethodDetails]
    linked_flows: LinkedFlows
    livemode: bool
    """
    Has the value `true` if the object exists in live mode or the value `false` if the object exists in test mode.
    """
    network: Literal["ach", "card", "stripe"]
    """
    The network used for the ReceivedDebit.
    """
    object: Literal["treasury.received_debit"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """
    reversal_details: Optional[ReversalDetails]
    """
    Details describing when a ReceivedDebit might be reversed.
    """
    status: Literal["failed", "succeeded"]
    """
    Status of the ReceivedDebit. ReceivedDebits are created with a status of either `succeeded` (approved) or `failed` (declined). The failure reason can be found under the `failure_code`.
    """
    transaction: Optional[ExpandableField["Transaction"]]
    """
    The Transaction associated with this object.
    """

    @classmethod
    def list(
        cls, **params: Unpack["ReceivedDebit.ListParams"]
    ) -> ListObject["ReceivedDebit"]:
        """
        Returns a list of ReceivedDebits.
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
        cls, **params: Unpack["ReceivedDebit.ListParams"]
    ) -> ListObject["ReceivedDebit"]:
        """
        Returns a list of ReceivedDebits.
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
        cls, id: str, **params: Unpack["ReceivedDebit.RetrieveParams"]
    ) -> "ReceivedDebit":
        """
        Retrieves the details of an existing ReceivedDebit by passing the unique ReceivedDebit ID from the ReceivedDebit list
        """
        instance = cls(id, **params)
        instance.refresh()
        return instance

    @classmethod
    async def retrieve_async(
        cls, id: str, **params: Unpack["ReceivedDebit.RetrieveParams"]
    ) -> "ReceivedDebit":
        """
        Retrieves the details of an existing ReceivedDebit by passing the unique ReceivedDebit ID from the ReceivedDebit list
        """
        instance = cls(id, **params)
        await instance.refresh_async()
        return instance

    class TestHelpers(APIResourceTestHelpers["ReceivedDebit"]):
        _resource_cls: Type["ReceivedDebit"]

        @classmethod
        def create(
            cls, **params: Unpack["ReceivedDebit.CreateParams"]
        ) -> "ReceivedDebit":
            """
            Use this endpoint to simulate a test mode ReceivedDebit initiated by a third party. In live mode, you can't directly create ReceivedDebits initiated by third parties.
            """
            return cast(
                "ReceivedDebit",
                cls._static_request(
                    "post",
                    "/v1/test_helpers/treasury/received_debits",
                    params=params,
                ),
            )

        @classmethod
        async def create_async(
            cls, **params: Unpack["ReceivedDebit.CreateParams"]
        ) -> "ReceivedDebit":
            """
            Use this endpoint to simulate a test mode ReceivedDebit initiated by a third party. In live mode, you can't directly create ReceivedDebits initiated by third parties.
            """
            return cast(
                "ReceivedDebit",
                await cls._static_request_async(
                    "post",
                    "/v1/test_helpers/treasury/received_debits",
                    params=params,
                ),
            )

    @property
    def test_helpers(self):
        return self.TestHelpers(self)

    _inner_class_types = {
        "initiating_payment_method_details": InitiatingPaymentMethodDetails,
        "linked_flows": LinkedFlows,
        "reversal_details": ReversalDetails,
    }


ReceivedDebit.TestHelpers._resource_cls = ReceivedDebit

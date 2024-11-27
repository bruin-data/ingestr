# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._createable_api_resource import CreateableAPIResource
from stripe._expandable_field import ExpandableField
from stripe._list_object import ListObject
from stripe._listable_api_resource import ListableAPIResource
from stripe._request_options import RequestOptions
from stripe._stripe_object import StripeObject
from stripe._test_helpers import APIResourceTestHelpers
from stripe._util import class_method_variant, sanitize_id
from typing import ClassVar, Dict, List, Optional, cast, overload
from typing_extensions import (
    Literal,
    NotRequired,
    Type,
    TypedDict,
    Unpack,
    TYPE_CHECKING,
)

if TYPE_CHECKING:
    from stripe._mandate import Mandate
    from stripe.treasury._transaction import Transaction


class OutboundPayment(
    CreateableAPIResource["OutboundPayment"],
    ListableAPIResource["OutboundPayment"],
):
    """
    Use OutboundPayments to send funds to another party's external bank account or [FinancialAccount](https://stripe.com/docs/api#financial_accounts). To send money to an account belonging to the same user, use an [OutboundTransfer](https://stripe.com/docs/api#outbound_transfers).

    Simulate OutboundPayment state changes with the `/v1/test_helpers/treasury/outbound_payments` endpoints. These methods can only be called on test mode objects.
    """

    OBJECT_NAME: ClassVar[Literal["treasury.outbound_payment"]] = (
        "treasury.outbound_payment"
    )

    class DestinationPaymentMethodDetails(StripeObject):
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
            Token of the FinancialAccount.
            """
            network: Literal["stripe"]
            """
            The rails used to send funds.
            """

        class UsBankAccount(StripeObject):
            account_holder_type: Optional[Literal["company", "individual"]]
            """
            Account holder type: individual or company.
            """
            account_type: Optional[Literal["checking", "savings"]]
            """
            Account type: checkings or savings. Defaults to checking if omitted.
            """
            bank_name: Optional[str]
            """
            Name of the bank associated with the bank account.
            """
            fingerprint: Optional[str]
            """
            Uniquely identifies this particular bank account. You can use this attribute to check whether two bank accounts are the same.
            """
            last4: Optional[str]
            """
            Last four digits of the bank account number.
            """
            mandate: Optional[ExpandableField["Mandate"]]
            """
            ID of the mandate used to make this payment.
            """
            network: Literal["ach", "us_domestic_wire"]
            """
            The network rails used. See the [docs](https://stripe.com/docs/treasury/money-movement/timelines) to learn more about money movement timelines for each network type.
            """
            routing_number: Optional[str]
            """
            Routing number of the bank account.
            """

        billing_details: BillingDetails
        financial_account: Optional[FinancialAccount]
        type: Literal["financial_account", "us_bank_account"]
        """
        The type of the payment method used in the OutboundPayment.
        """
        us_bank_account: Optional[UsBankAccount]
        _inner_class_types = {
            "billing_details": BillingDetails,
            "financial_account": FinancialAccount,
            "us_bank_account": UsBankAccount,
        }

    class EndUserDetails(StripeObject):
        ip_address: Optional[str]
        """
        IP address of the user initiating the OutboundPayment. Set if `present` is set to `true`. IP address collection is required for risk and compliance reasons. This will be used to help determine if the OutboundPayment is authorized or should be blocked.
        """
        present: bool
        """
        `true` if the OutboundPayment creation request is being made on behalf of an end user by a platform. Otherwise, `false`.
        """

    class ReturnedDetails(StripeObject):
        code: Literal[
            "account_closed",
            "account_frozen",
            "bank_account_restricted",
            "bank_ownership_changed",
            "declined",
            "incorrect_account_holder_name",
            "invalid_account_number",
            "invalid_currency",
            "no_account",
            "other",
        ]
        """
        Reason for the return.
        """
        transaction: ExpandableField["Transaction"]
        """
        The Transaction associated with this object.
        """

    class StatusTransitions(StripeObject):
        canceled_at: Optional[int]
        """
        Timestamp describing when an OutboundPayment changed status to `canceled`.
        """
        failed_at: Optional[int]
        """
        Timestamp describing when an OutboundPayment changed status to `failed`.
        """
        posted_at: Optional[int]
        """
        Timestamp describing when an OutboundPayment changed status to `posted`.
        """
        returned_at: Optional[int]
        """
        Timestamp describing when an OutboundPayment changed status to `returned`.
        """

    class TrackingDetails(StripeObject):
        class Ach(StripeObject):
            trace_id: str
            """
            ACH trace ID of the OutboundPayment for payments sent over the `ach` network.
            """

        class UsDomesticWire(StripeObject):
            imad: str
            """
            IMAD of the OutboundPayment for payments sent over the `us_domestic_wire` network.
            """
            omad: Optional[str]
            """
            OMAD of the OutboundPayment for payments sent over the `us_domestic_wire` network.
            """

        ach: Optional[Ach]
        type: Literal["ach", "us_domestic_wire"]
        """
        The US bank account network used to send funds.
        """
        us_domestic_wire: Optional[UsDomesticWire]
        _inner_class_types = {"ach": Ach, "us_domestic_wire": UsDomesticWire}

    class CancelParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
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
            "OutboundPayment.CreateParamsDestinationPaymentMethodData"
        ]
        """
        Hash used to generate the PaymentMethod to be used for this OutboundPayment. Exclusive with `destination_payment_method`.
        """
        destination_payment_method_options: NotRequired[
            "OutboundPayment.CreateParamsDestinationPaymentMethodOptions"
        ]
        """
        Payment method-specific configuration for this OutboundPayment.
        """
        end_user_details: NotRequired[
            "OutboundPayment.CreateParamsEndUserDetails"
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
            "OutboundPayment.CreateParamsDestinationPaymentMethodDataBillingDetails"
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
            "OutboundPayment.CreateParamsDestinationPaymentMethodDataUsBankAccount"
        ]
        """
        Required hash if type is set to `us_bank_account`.
        """

    class CreateParamsDestinationPaymentMethodDataBillingDetails(TypedDict):
        address: NotRequired[
            "Literal['']|OutboundPayment.CreateParamsDestinationPaymentMethodDataBillingDetailsAddress"
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
            "Literal['']|OutboundPayment.CreateParamsDestinationPaymentMethodOptionsUsBankAccount"
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

    class FailParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class ListParams(RequestOptions):
        created: NotRequired["OutboundPayment.ListParamsCreated|int"]
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

    class PostParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class RetrieveParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class ReturnOutboundPaymentParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        returned_details: NotRequired[
            "OutboundPayment.ReturnOutboundPaymentParamsReturnedDetails"
        ]
        """
        Optional hash to set the return code.
        """

    class ReturnOutboundPaymentParamsReturnedDetails(TypedDict):
        code: NotRequired[
            Literal[
                "account_closed",
                "account_frozen",
                "bank_account_restricted",
                "bank_ownership_changed",
                "declined",
                "incorrect_account_holder_name",
                "invalid_account_number",
                "invalid_currency",
                "no_account",
                "other",
            ]
        ]
        """
        The return code to be set on the OutboundPayment object.
        """

    class UpdateParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        tracking_details: "OutboundPayment.UpdateParamsTrackingDetails"
        """
        Details about network-specific tracking information.
        """

    class UpdateParamsTrackingDetails(TypedDict):
        ach: NotRequired["OutboundPayment.UpdateParamsTrackingDetailsAch"]
        """
        ACH network tracking details.
        """
        type: Literal["ach", "us_domestic_wire"]
        """
        The US bank account network used to send funds.
        """
        us_domestic_wire: NotRequired[
            "OutboundPayment.UpdateParamsTrackingDetailsUsDomesticWire"
        ]
        """
        US domestic wire network tracking details.
        """

    class UpdateParamsTrackingDetailsAch(TypedDict):
        trace_id: str
        """
        ACH trace ID for funds sent over the `ach` network.
        """

    class UpdateParamsTrackingDetailsUsDomesticWire(TypedDict):
        imad: NotRequired[str]
        """
        IMAD for funds sent over the `us_domestic_wire` network.
        """
        omad: NotRequired[str]
        """
        OMAD for funds sent over the `us_domestic_wire` network.
        """

    amount: int
    """
    Amount (in cents) transferred.
    """
    cancelable: bool
    """
    Returns `true` if the object can be canceled, and `false` otherwise.
    """
    created: int
    """
    Time at which the object was created. Measured in seconds since the Unix epoch.
    """
    currency: str
    """
    Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
    """
    customer: Optional[str]
    """
    ID of the [customer](https://stripe.com/docs/api/customers) to whom an OutboundPayment is sent.
    """
    description: Optional[str]
    """
    An arbitrary string attached to the object. Often useful for displaying to users.
    """
    destination_payment_method: Optional[str]
    """
    The PaymentMethod via which an OutboundPayment is sent. This field can be empty if the OutboundPayment was created using `destination_payment_method_data`.
    """
    destination_payment_method_details: Optional[
        DestinationPaymentMethodDetails
    ]
    """
    Details about the PaymentMethod for an OutboundPayment.
    """
    end_user_details: Optional[EndUserDetails]
    """
    Details about the end user.
    """
    expected_arrival_date: int
    """
    The date when funds are expected to arrive in the destination account.
    """
    financial_account: str
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
    livemode: bool
    """
    Has the value `true` if the object exists in live mode or the value `false` if the object exists in test mode.
    """
    metadata: Dict[str, str]
    """
    Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format.
    """
    object: Literal["treasury.outbound_payment"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """
    returned_details: Optional[ReturnedDetails]
    """
    Details about a returned OutboundPayment. Only set when the status is `returned`.
    """
    statement_descriptor: str
    """
    The description that appears on the receiving end for an OutboundPayment (for example, bank statement for external bank transfer).
    """
    status: Literal["canceled", "failed", "posted", "processing", "returned"]
    """
    Current status of the OutboundPayment: `processing`, `failed`, `posted`, `returned`, `canceled`. An OutboundPayment is `processing` if it has been created and is pending. The status changes to `posted` once the OutboundPayment has been "confirmed" and funds have left the account, or to `failed` or `canceled`. If an OutboundPayment fails to arrive at its destination, its status will change to `returned`.
    """
    status_transitions: StatusTransitions
    tracking_details: Optional[TrackingDetails]
    """
    Details about network-specific tracking information if available.
    """
    transaction: ExpandableField["Transaction"]
    """
    The Transaction associated with this object.
    """

    @classmethod
    def _cls_cancel(
        cls, id: str, **params: Unpack["OutboundPayment.CancelParams"]
    ) -> "OutboundPayment":
        """
        Cancel an OutboundPayment.
        """
        return cast(
            "OutboundPayment",
            cls._static_request(
                "post",
                "/v1/treasury/outbound_payments/{id}/cancel".format(
                    id=sanitize_id(id)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    def cancel(
        id: str, **params: Unpack["OutboundPayment.CancelParams"]
    ) -> "OutboundPayment":
        """
        Cancel an OutboundPayment.
        """
        ...

    @overload
    def cancel(
        self, **params: Unpack["OutboundPayment.CancelParams"]
    ) -> "OutboundPayment":
        """
        Cancel an OutboundPayment.
        """
        ...

    @class_method_variant("_cls_cancel")
    def cancel(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["OutboundPayment.CancelParams"]
    ) -> "OutboundPayment":
        """
        Cancel an OutboundPayment.
        """
        return cast(
            "OutboundPayment",
            self._request(
                "post",
                "/v1/treasury/outbound_payments/{id}/cancel".format(
                    id=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    async def _cls_cancel_async(
        cls, id: str, **params: Unpack["OutboundPayment.CancelParams"]
    ) -> "OutboundPayment":
        """
        Cancel an OutboundPayment.
        """
        return cast(
            "OutboundPayment",
            await cls._static_request_async(
                "post",
                "/v1/treasury/outbound_payments/{id}/cancel".format(
                    id=sanitize_id(id)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    async def cancel_async(
        id: str, **params: Unpack["OutboundPayment.CancelParams"]
    ) -> "OutboundPayment":
        """
        Cancel an OutboundPayment.
        """
        ...

    @overload
    async def cancel_async(
        self, **params: Unpack["OutboundPayment.CancelParams"]
    ) -> "OutboundPayment":
        """
        Cancel an OutboundPayment.
        """
        ...

    @class_method_variant("_cls_cancel_async")
    async def cancel_async(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["OutboundPayment.CancelParams"]
    ) -> "OutboundPayment":
        """
        Cancel an OutboundPayment.
        """
        return cast(
            "OutboundPayment",
            await self._request_async(
                "post",
                "/v1/treasury/outbound_payments/{id}/cancel".format(
                    id=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    def create(
        cls, **params: Unpack["OutboundPayment.CreateParams"]
    ) -> "OutboundPayment":
        """
        Creates an OutboundPayment.
        """
        return cast(
            "OutboundPayment",
            cls._static_request(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    async def create_async(
        cls, **params: Unpack["OutboundPayment.CreateParams"]
    ) -> "OutboundPayment":
        """
        Creates an OutboundPayment.
        """
        return cast(
            "OutboundPayment",
            await cls._static_request_async(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    def list(
        cls, **params: Unpack["OutboundPayment.ListParams"]
    ) -> ListObject["OutboundPayment"]:
        """
        Returns a list of OutboundPayments sent from the specified FinancialAccount.
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
        cls, **params: Unpack["OutboundPayment.ListParams"]
    ) -> ListObject["OutboundPayment"]:
        """
        Returns a list of OutboundPayments sent from the specified FinancialAccount.
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
        cls, id: str, **params: Unpack["OutboundPayment.RetrieveParams"]
    ) -> "OutboundPayment":
        """
        Retrieves the details of an existing OutboundPayment by passing the unique OutboundPayment ID from either the OutboundPayment creation request or OutboundPayment list.
        """
        instance = cls(id, **params)
        instance.refresh()
        return instance

    @classmethod
    async def retrieve_async(
        cls, id: str, **params: Unpack["OutboundPayment.RetrieveParams"]
    ) -> "OutboundPayment":
        """
        Retrieves the details of an existing OutboundPayment by passing the unique OutboundPayment ID from either the OutboundPayment creation request or OutboundPayment list.
        """
        instance = cls(id, **params)
        await instance.refresh_async()
        return instance

    class TestHelpers(APIResourceTestHelpers["OutboundPayment"]):
        _resource_cls: Type["OutboundPayment"]

        @classmethod
        def _cls_fail(
            cls, id: str, **params: Unpack["OutboundPayment.FailParams"]
        ) -> "OutboundPayment":
            """
            Transitions a test mode created OutboundPayment to the failed status. The OutboundPayment must already be in the processing state.
            """
            return cast(
                "OutboundPayment",
                cls._static_request(
                    "post",
                    "/v1/test_helpers/treasury/outbound_payments/{id}/fail".format(
                        id=sanitize_id(id)
                    ),
                    params=params,
                ),
            )

        @overload
        @staticmethod
        def fail(
            id: str, **params: Unpack["OutboundPayment.FailParams"]
        ) -> "OutboundPayment":
            """
            Transitions a test mode created OutboundPayment to the failed status. The OutboundPayment must already be in the processing state.
            """
            ...

        @overload
        def fail(
            self, **params: Unpack["OutboundPayment.FailParams"]
        ) -> "OutboundPayment":
            """
            Transitions a test mode created OutboundPayment to the failed status. The OutboundPayment must already be in the processing state.
            """
            ...

        @class_method_variant("_cls_fail")
        def fail(  # pyright: ignore[reportGeneralTypeIssues]
            self, **params: Unpack["OutboundPayment.FailParams"]
        ) -> "OutboundPayment":
            """
            Transitions a test mode created OutboundPayment to the failed status. The OutboundPayment must already be in the processing state.
            """
            return cast(
                "OutboundPayment",
                self.resource._request(
                    "post",
                    "/v1/test_helpers/treasury/outbound_payments/{id}/fail".format(
                        id=sanitize_id(self.resource.get("id"))
                    ),
                    params=params,
                ),
            )

        @classmethod
        async def _cls_fail_async(
            cls, id: str, **params: Unpack["OutboundPayment.FailParams"]
        ) -> "OutboundPayment":
            """
            Transitions a test mode created OutboundPayment to the failed status. The OutboundPayment must already be in the processing state.
            """
            return cast(
                "OutboundPayment",
                await cls._static_request_async(
                    "post",
                    "/v1/test_helpers/treasury/outbound_payments/{id}/fail".format(
                        id=sanitize_id(id)
                    ),
                    params=params,
                ),
            )

        @overload
        @staticmethod
        async def fail_async(
            id: str, **params: Unpack["OutboundPayment.FailParams"]
        ) -> "OutboundPayment":
            """
            Transitions a test mode created OutboundPayment to the failed status. The OutboundPayment must already be in the processing state.
            """
            ...

        @overload
        async def fail_async(
            self, **params: Unpack["OutboundPayment.FailParams"]
        ) -> "OutboundPayment":
            """
            Transitions a test mode created OutboundPayment to the failed status. The OutboundPayment must already be in the processing state.
            """
            ...

        @class_method_variant("_cls_fail_async")
        async def fail_async(  # pyright: ignore[reportGeneralTypeIssues]
            self, **params: Unpack["OutboundPayment.FailParams"]
        ) -> "OutboundPayment":
            """
            Transitions a test mode created OutboundPayment to the failed status. The OutboundPayment must already be in the processing state.
            """
            return cast(
                "OutboundPayment",
                await self.resource._request_async(
                    "post",
                    "/v1/test_helpers/treasury/outbound_payments/{id}/fail".format(
                        id=sanitize_id(self.resource.get("id"))
                    ),
                    params=params,
                ),
            )

        @classmethod
        def _cls_post(
            cls, id: str, **params: Unpack["OutboundPayment.PostParams"]
        ) -> "OutboundPayment":
            """
            Transitions a test mode created OutboundPayment to the posted status. The OutboundPayment must already be in the processing state.
            """
            return cast(
                "OutboundPayment",
                cls._static_request(
                    "post",
                    "/v1/test_helpers/treasury/outbound_payments/{id}/post".format(
                        id=sanitize_id(id)
                    ),
                    params=params,
                ),
            )

        @overload
        @staticmethod
        def post(
            id: str, **params: Unpack["OutboundPayment.PostParams"]
        ) -> "OutboundPayment":
            """
            Transitions a test mode created OutboundPayment to the posted status. The OutboundPayment must already be in the processing state.
            """
            ...

        @overload
        def post(
            self, **params: Unpack["OutboundPayment.PostParams"]
        ) -> "OutboundPayment":
            """
            Transitions a test mode created OutboundPayment to the posted status. The OutboundPayment must already be in the processing state.
            """
            ...

        @class_method_variant("_cls_post")
        def post(  # pyright: ignore[reportGeneralTypeIssues]
            self, **params: Unpack["OutboundPayment.PostParams"]
        ) -> "OutboundPayment":
            """
            Transitions a test mode created OutboundPayment to the posted status. The OutboundPayment must already be in the processing state.
            """
            return cast(
                "OutboundPayment",
                self.resource._request(
                    "post",
                    "/v1/test_helpers/treasury/outbound_payments/{id}/post".format(
                        id=sanitize_id(self.resource.get("id"))
                    ),
                    params=params,
                ),
            )

        @classmethod
        async def _cls_post_async(
            cls, id: str, **params: Unpack["OutboundPayment.PostParams"]
        ) -> "OutboundPayment":
            """
            Transitions a test mode created OutboundPayment to the posted status. The OutboundPayment must already be in the processing state.
            """
            return cast(
                "OutboundPayment",
                await cls._static_request_async(
                    "post",
                    "/v1/test_helpers/treasury/outbound_payments/{id}/post".format(
                        id=sanitize_id(id)
                    ),
                    params=params,
                ),
            )

        @overload
        @staticmethod
        async def post_async(
            id: str, **params: Unpack["OutboundPayment.PostParams"]
        ) -> "OutboundPayment":
            """
            Transitions a test mode created OutboundPayment to the posted status. The OutboundPayment must already be in the processing state.
            """
            ...

        @overload
        async def post_async(
            self, **params: Unpack["OutboundPayment.PostParams"]
        ) -> "OutboundPayment":
            """
            Transitions a test mode created OutboundPayment to the posted status. The OutboundPayment must already be in the processing state.
            """
            ...

        @class_method_variant("_cls_post_async")
        async def post_async(  # pyright: ignore[reportGeneralTypeIssues]
            self, **params: Unpack["OutboundPayment.PostParams"]
        ) -> "OutboundPayment":
            """
            Transitions a test mode created OutboundPayment to the posted status. The OutboundPayment must already be in the processing state.
            """
            return cast(
                "OutboundPayment",
                await self.resource._request_async(
                    "post",
                    "/v1/test_helpers/treasury/outbound_payments/{id}/post".format(
                        id=sanitize_id(self.resource.get("id"))
                    ),
                    params=params,
                ),
            )

        @classmethod
        def _cls_return_outbound_payment(
            cls,
            id: str,
            **params: Unpack["OutboundPayment.ReturnOutboundPaymentParams"],
        ) -> "OutboundPayment":
            """
            Transitions a test mode created OutboundPayment to the returned status. The OutboundPayment must already be in the processing state.
            """
            return cast(
                "OutboundPayment",
                cls._static_request(
                    "post",
                    "/v1/test_helpers/treasury/outbound_payments/{id}/return".format(
                        id=sanitize_id(id)
                    ),
                    params=params,
                ),
            )

        @overload
        @staticmethod
        def return_outbound_payment(
            id: str,
            **params: Unpack["OutboundPayment.ReturnOutboundPaymentParams"],
        ) -> "OutboundPayment":
            """
            Transitions a test mode created OutboundPayment to the returned status. The OutboundPayment must already be in the processing state.
            """
            ...

        @overload
        def return_outbound_payment(
            self,
            **params: Unpack["OutboundPayment.ReturnOutboundPaymentParams"],
        ) -> "OutboundPayment":
            """
            Transitions a test mode created OutboundPayment to the returned status. The OutboundPayment must already be in the processing state.
            """
            ...

        @class_method_variant("_cls_return_outbound_payment")
        def return_outbound_payment(  # pyright: ignore[reportGeneralTypeIssues]
            self,
            **params: Unpack["OutboundPayment.ReturnOutboundPaymentParams"],
        ) -> "OutboundPayment":
            """
            Transitions a test mode created OutboundPayment to the returned status. The OutboundPayment must already be in the processing state.
            """
            return cast(
                "OutboundPayment",
                self.resource._request(
                    "post",
                    "/v1/test_helpers/treasury/outbound_payments/{id}/return".format(
                        id=sanitize_id(self.resource.get("id"))
                    ),
                    params=params,
                ),
            )

        @classmethod
        async def _cls_return_outbound_payment_async(
            cls,
            id: str,
            **params: Unpack["OutboundPayment.ReturnOutboundPaymentParams"],
        ) -> "OutboundPayment":
            """
            Transitions a test mode created OutboundPayment to the returned status. The OutboundPayment must already be in the processing state.
            """
            return cast(
                "OutboundPayment",
                await cls._static_request_async(
                    "post",
                    "/v1/test_helpers/treasury/outbound_payments/{id}/return".format(
                        id=sanitize_id(id)
                    ),
                    params=params,
                ),
            )

        @overload
        @staticmethod
        async def return_outbound_payment_async(
            id: str,
            **params: Unpack["OutboundPayment.ReturnOutboundPaymentParams"],
        ) -> "OutboundPayment":
            """
            Transitions a test mode created OutboundPayment to the returned status. The OutboundPayment must already be in the processing state.
            """
            ...

        @overload
        async def return_outbound_payment_async(
            self,
            **params: Unpack["OutboundPayment.ReturnOutboundPaymentParams"],
        ) -> "OutboundPayment":
            """
            Transitions a test mode created OutboundPayment to the returned status. The OutboundPayment must already be in the processing state.
            """
            ...

        @class_method_variant("_cls_return_outbound_payment_async")
        async def return_outbound_payment_async(  # pyright: ignore[reportGeneralTypeIssues]
            self,
            **params: Unpack["OutboundPayment.ReturnOutboundPaymentParams"],
        ) -> "OutboundPayment":
            """
            Transitions a test mode created OutboundPayment to the returned status. The OutboundPayment must already be in the processing state.
            """
            return cast(
                "OutboundPayment",
                await self.resource._request_async(
                    "post",
                    "/v1/test_helpers/treasury/outbound_payments/{id}/return".format(
                        id=sanitize_id(self.resource.get("id"))
                    ),
                    params=params,
                ),
            )

        @classmethod
        def _cls_update(
            cls, id: str, **params: Unpack["OutboundPayment.UpdateParams"]
        ) -> "OutboundPayment":
            """
            Updates a test mode created OutboundPayment with tracking details. The OutboundPayment must not be cancelable, and cannot be in the canceled or failed states.
            """
            return cast(
                "OutboundPayment",
                cls._static_request(
                    "post",
                    "/v1/test_helpers/treasury/outbound_payments/{id}".format(
                        id=sanitize_id(id)
                    ),
                    params=params,
                ),
            )

        @overload
        @staticmethod
        def update(
            id: str, **params: Unpack["OutboundPayment.UpdateParams"]
        ) -> "OutboundPayment":
            """
            Updates a test mode created OutboundPayment with tracking details. The OutboundPayment must not be cancelable, and cannot be in the canceled or failed states.
            """
            ...

        @overload
        def update(
            self, **params: Unpack["OutboundPayment.UpdateParams"]
        ) -> "OutboundPayment":
            """
            Updates a test mode created OutboundPayment with tracking details. The OutboundPayment must not be cancelable, and cannot be in the canceled or failed states.
            """
            ...

        @class_method_variant("_cls_update")
        def update(  # pyright: ignore[reportGeneralTypeIssues]
            self, **params: Unpack["OutboundPayment.UpdateParams"]
        ) -> "OutboundPayment":
            """
            Updates a test mode created OutboundPayment with tracking details. The OutboundPayment must not be cancelable, and cannot be in the canceled or failed states.
            """
            return cast(
                "OutboundPayment",
                self.resource._request(
                    "post",
                    "/v1/test_helpers/treasury/outbound_payments/{id}".format(
                        id=sanitize_id(self.resource.get("id"))
                    ),
                    params=params,
                ),
            )

        @classmethod
        async def _cls_update_async(
            cls, id: str, **params: Unpack["OutboundPayment.UpdateParams"]
        ) -> "OutboundPayment":
            """
            Updates a test mode created OutboundPayment with tracking details. The OutboundPayment must not be cancelable, and cannot be in the canceled or failed states.
            """
            return cast(
                "OutboundPayment",
                await cls._static_request_async(
                    "post",
                    "/v1/test_helpers/treasury/outbound_payments/{id}".format(
                        id=sanitize_id(id)
                    ),
                    params=params,
                ),
            )

        @overload
        @staticmethod
        async def update_async(
            id: str, **params: Unpack["OutboundPayment.UpdateParams"]
        ) -> "OutboundPayment":
            """
            Updates a test mode created OutboundPayment with tracking details. The OutboundPayment must not be cancelable, and cannot be in the canceled or failed states.
            """
            ...

        @overload
        async def update_async(
            self, **params: Unpack["OutboundPayment.UpdateParams"]
        ) -> "OutboundPayment":
            """
            Updates a test mode created OutboundPayment with tracking details. The OutboundPayment must not be cancelable, and cannot be in the canceled or failed states.
            """
            ...

        @class_method_variant("_cls_update_async")
        async def update_async(  # pyright: ignore[reportGeneralTypeIssues]
            self, **params: Unpack["OutboundPayment.UpdateParams"]
        ) -> "OutboundPayment":
            """
            Updates a test mode created OutboundPayment with tracking details. The OutboundPayment must not be cancelable, and cannot be in the canceled or failed states.
            """
            return cast(
                "OutboundPayment",
                await self.resource._request_async(
                    "post",
                    "/v1/test_helpers/treasury/outbound_payments/{id}".format(
                        id=sanitize_id(self.resource.get("id"))
                    ),
                    params=params,
                ),
            )

    @property
    def test_helpers(self):
        return self.TestHelpers(self)

    _inner_class_types = {
        "destination_payment_method_details": DestinationPaymentMethodDetails,
        "end_user_details": EndUserDetails,
        "returned_details": ReturnedDetails,
        "status_transitions": StatusTransitions,
        "tracking_details": TrackingDetails,
    }


OutboundPayment.TestHelpers._resource_cls = OutboundPayment

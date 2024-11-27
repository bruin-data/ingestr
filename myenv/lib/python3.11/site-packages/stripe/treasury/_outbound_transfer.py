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


class OutboundTransfer(
    CreateableAPIResource["OutboundTransfer"],
    ListableAPIResource["OutboundTransfer"],
):
    """
    Use OutboundTransfers to transfer funds from a [FinancialAccount](https://stripe.com/docs/api#financial_accounts) to a PaymentMethod belonging to the same entity. To send funds to a different party, use [OutboundPayments](https://stripe.com/docs/api#outbound_payments) instead. You can send funds over ACH rails or through a domestic wire transfer to a user's own external bank account.

    Simulate OutboundTransfer state changes with the `/v1/test_helpers/treasury/outbound_transfers` endpoints. These methods can only be called on test mode objects.
    """

    OBJECT_NAME: ClassVar[Literal["treasury.outbound_transfer"]] = (
        "treasury.outbound_transfer"
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
        type: Literal["us_bank_account"]
        """
        The type of the payment method used in the OutboundTransfer.
        """
        us_bank_account: Optional[UsBankAccount]
        _inner_class_types = {
            "billing_details": BillingDetails,
            "us_bank_account": UsBankAccount,
        }

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
        Timestamp describing when an OutboundTransfer changed status to `canceled`
        """
        failed_at: Optional[int]
        """
        Timestamp describing when an OutboundTransfer changed status to `failed`
        """
        posted_at: Optional[int]
        """
        Timestamp describing when an OutboundTransfer changed status to `posted`
        """
        returned_at: Optional[int]
        """
        Timestamp describing when an OutboundTransfer changed status to `returned`
        """

    class TrackingDetails(StripeObject):
        class Ach(StripeObject):
            trace_id: str
            """
            ACH trace ID of the OutboundTransfer for transfers sent over the `ach` network.
            """

        class UsDomesticWire(StripeObject):
            imad: str
            """
            IMAD of the OutboundTransfer for transfers sent over the `us_domestic_wire` network.
            """
            omad: Optional[str]
            """
            OMAD of the OutboundTransfer for transfers sent over the `us_domestic_wire` network.
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
        description: NotRequired[str]
        """
        An arbitrary string attached to the object. Often useful for displaying to users.
        """
        destination_payment_method: NotRequired[str]
        """
        The PaymentMethod to use as the payment instrument for the OutboundTransfer.
        """
        destination_payment_method_options: NotRequired[
            "OutboundTransfer.CreateParamsDestinationPaymentMethodOptions"
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
            "Literal['']|OutboundTransfer.CreateParamsDestinationPaymentMethodOptionsUsBankAccount"
        ]
        """
        Optional fields for `us_bank_account`.
        """

    class CreateParamsDestinationPaymentMethodOptionsUsBankAccount(TypedDict):
        network: NotRequired[Literal["ach", "us_domestic_wire"]]
        """
        Specifies the network rails to be used. If not set, will default to the PaymentMethod's preferred network. See the [docs](https://stripe.com/docs/treasury/money-movement/timelines) to learn more about money movement timelines for each network type.
        """

    class FailParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
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

    class ReturnOutboundTransferParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        returned_details: NotRequired[
            "OutboundTransfer.ReturnOutboundTransferParamsReturnedDetails"
        ]
        """
        Details about a returned OutboundTransfer.
        """

    class ReturnOutboundTransferParamsReturnedDetails(TypedDict):
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
        Reason for the return.
        """

    class UpdateParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        tracking_details: "OutboundTransfer.UpdateParamsTrackingDetails"
        """
        Details about network-specific tracking information.
        """

    class UpdateParamsTrackingDetails(TypedDict):
        ach: NotRequired["OutboundTransfer.UpdateParamsTrackingDetailsAch"]
        """
        ACH network tracking details.
        """
        type: Literal["ach", "us_domestic_wire"]
        """
        The US bank account network used to send funds.
        """
        us_domestic_wire: NotRequired[
            "OutboundTransfer.UpdateParamsTrackingDetailsUsDomesticWire"
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
    description: Optional[str]
    """
    An arbitrary string attached to the object. Often useful for displaying to users.
    """
    destination_payment_method: Optional[str]
    """
    The PaymentMethod used as the payment instrument for an OutboundTransfer.
    """
    destination_payment_method_details: DestinationPaymentMethodDetails
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
    object: Literal["treasury.outbound_transfer"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """
    returned_details: Optional[ReturnedDetails]
    """
    Details about a returned OutboundTransfer. Only set when the status is `returned`.
    """
    statement_descriptor: str
    """
    Information about the OutboundTransfer to be sent to the recipient account.
    """
    status: Literal["canceled", "failed", "posted", "processing", "returned"]
    """
    Current status of the OutboundTransfer: `processing`, `failed`, `canceled`, `posted`, `returned`. An OutboundTransfer is `processing` if it has been created and is pending. The status changes to `posted` once the OutboundTransfer has been "confirmed" and funds have left the account, or to `failed` or `canceled`. If an OutboundTransfer fails to arrive at its destination, its status will change to `returned`.
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
        cls,
        outbound_transfer: str,
        **params: Unpack["OutboundTransfer.CancelParams"],
    ) -> "OutboundTransfer":
        """
        An OutboundTransfer can be canceled if the funds have not yet been paid out.
        """
        return cast(
            "OutboundTransfer",
            cls._static_request(
                "post",
                "/v1/treasury/outbound_transfers/{outbound_transfer}/cancel".format(
                    outbound_transfer=sanitize_id(outbound_transfer)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    def cancel(
        outbound_transfer: str,
        **params: Unpack["OutboundTransfer.CancelParams"],
    ) -> "OutboundTransfer":
        """
        An OutboundTransfer can be canceled if the funds have not yet been paid out.
        """
        ...

    @overload
    def cancel(
        self, **params: Unpack["OutboundTransfer.CancelParams"]
    ) -> "OutboundTransfer":
        """
        An OutboundTransfer can be canceled if the funds have not yet been paid out.
        """
        ...

    @class_method_variant("_cls_cancel")
    def cancel(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["OutboundTransfer.CancelParams"]
    ) -> "OutboundTransfer":
        """
        An OutboundTransfer can be canceled if the funds have not yet been paid out.
        """
        return cast(
            "OutboundTransfer",
            self._request(
                "post",
                "/v1/treasury/outbound_transfers/{outbound_transfer}/cancel".format(
                    outbound_transfer=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    async def _cls_cancel_async(
        cls,
        outbound_transfer: str,
        **params: Unpack["OutboundTransfer.CancelParams"],
    ) -> "OutboundTransfer":
        """
        An OutboundTransfer can be canceled if the funds have not yet been paid out.
        """
        return cast(
            "OutboundTransfer",
            await cls._static_request_async(
                "post",
                "/v1/treasury/outbound_transfers/{outbound_transfer}/cancel".format(
                    outbound_transfer=sanitize_id(outbound_transfer)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    async def cancel_async(
        outbound_transfer: str,
        **params: Unpack["OutboundTransfer.CancelParams"],
    ) -> "OutboundTransfer":
        """
        An OutboundTransfer can be canceled if the funds have not yet been paid out.
        """
        ...

    @overload
    async def cancel_async(
        self, **params: Unpack["OutboundTransfer.CancelParams"]
    ) -> "OutboundTransfer":
        """
        An OutboundTransfer can be canceled if the funds have not yet been paid out.
        """
        ...

    @class_method_variant("_cls_cancel_async")
    async def cancel_async(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["OutboundTransfer.CancelParams"]
    ) -> "OutboundTransfer":
        """
        An OutboundTransfer can be canceled if the funds have not yet been paid out.
        """
        return cast(
            "OutboundTransfer",
            await self._request_async(
                "post",
                "/v1/treasury/outbound_transfers/{outbound_transfer}/cancel".format(
                    outbound_transfer=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    def create(
        cls, **params: Unpack["OutboundTransfer.CreateParams"]
    ) -> "OutboundTransfer":
        """
        Creates an OutboundTransfer.
        """
        return cast(
            "OutboundTransfer",
            cls._static_request(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    async def create_async(
        cls, **params: Unpack["OutboundTransfer.CreateParams"]
    ) -> "OutboundTransfer":
        """
        Creates an OutboundTransfer.
        """
        return cast(
            "OutboundTransfer",
            await cls._static_request_async(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    def list(
        cls, **params: Unpack["OutboundTransfer.ListParams"]
    ) -> ListObject["OutboundTransfer"]:
        """
        Returns a list of OutboundTransfers sent from the specified FinancialAccount.
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
        cls, **params: Unpack["OutboundTransfer.ListParams"]
    ) -> ListObject["OutboundTransfer"]:
        """
        Returns a list of OutboundTransfers sent from the specified FinancialAccount.
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
        cls, id: str, **params: Unpack["OutboundTransfer.RetrieveParams"]
    ) -> "OutboundTransfer":
        """
        Retrieves the details of an existing OutboundTransfer by passing the unique OutboundTransfer ID from either the OutboundTransfer creation request or OutboundTransfer list.
        """
        instance = cls(id, **params)
        instance.refresh()
        return instance

    @classmethod
    async def retrieve_async(
        cls, id: str, **params: Unpack["OutboundTransfer.RetrieveParams"]
    ) -> "OutboundTransfer":
        """
        Retrieves the details of an existing OutboundTransfer by passing the unique OutboundTransfer ID from either the OutboundTransfer creation request or OutboundTransfer list.
        """
        instance = cls(id, **params)
        await instance.refresh_async()
        return instance

    class TestHelpers(APIResourceTestHelpers["OutboundTransfer"]):
        _resource_cls: Type["OutboundTransfer"]

        @classmethod
        def _cls_fail(
            cls,
            outbound_transfer: str,
            **params: Unpack["OutboundTransfer.FailParams"],
        ) -> "OutboundTransfer":
            """
            Transitions a test mode created OutboundTransfer to the failed status. The OutboundTransfer must already be in the processing state.
            """
            return cast(
                "OutboundTransfer",
                cls._static_request(
                    "post",
                    "/v1/test_helpers/treasury/outbound_transfers/{outbound_transfer}/fail".format(
                        outbound_transfer=sanitize_id(outbound_transfer)
                    ),
                    params=params,
                ),
            )

        @overload
        @staticmethod
        def fail(
            outbound_transfer: str,
            **params: Unpack["OutboundTransfer.FailParams"],
        ) -> "OutboundTransfer":
            """
            Transitions a test mode created OutboundTransfer to the failed status. The OutboundTransfer must already be in the processing state.
            """
            ...

        @overload
        def fail(
            self, **params: Unpack["OutboundTransfer.FailParams"]
        ) -> "OutboundTransfer":
            """
            Transitions a test mode created OutboundTransfer to the failed status. The OutboundTransfer must already be in the processing state.
            """
            ...

        @class_method_variant("_cls_fail")
        def fail(  # pyright: ignore[reportGeneralTypeIssues]
            self, **params: Unpack["OutboundTransfer.FailParams"]
        ) -> "OutboundTransfer":
            """
            Transitions a test mode created OutboundTransfer to the failed status. The OutboundTransfer must already be in the processing state.
            """
            return cast(
                "OutboundTransfer",
                self.resource._request(
                    "post",
                    "/v1/test_helpers/treasury/outbound_transfers/{outbound_transfer}/fail".format(
                        outbound_transfer=sanitize_id(self.resource.get("id"))
                    ),
                    params=params,
                ),
            )

        @classmethod
        async def _cls_fail_async(
            cls,
            outbound_transfer: str,
            **params: Unpack["OutboundTransfer.FailParams"],
        ) -> "OutboundTransfer":
            """
            Transitions a test mode created OutboundTransfer to the failed status. The OutboundTransfer must already be in the processing state.
            """
            return cast(
                "OutboundTransfer",
                await cls._static_request_async(
                    "post",
                    "/v1/test_helpers/treasury/outbound_transfers/{outbound_transfer}/fail".format(
                        outbound_transfer=sanitize_id(outbound_transfer)
                    ),
                    params=params,
                ),
            )

        @overload
        @staticmethod
        async def fail_async(
            outbound_transfer: str,
            **params: Unpack["OutboundTransfer.FailParams"],
        ) -> "OutboundTransfer":
            """
            Transitions a test mode created OutboundTransfer to the failed status. The OutboundTransfer must already be in the processing state.
            """
            ...

        @overload
        async def fail_async(
            self, **params: Unpack["OutboundTransfer.FailParams"]
        ) -> "OutboundTransfer":
            """
            Transitions a test mode created OutboundTransfer to the failed status. The OutboundTransfer must already be in the processing state.
            """
            ...

        @class_method_variant("_cls_fail_async")
        async def fail_async(  # pyright: ignore[reportGeneralTypeIssues]
            self, **params: Unpack["OutboundTransfer.FailParams"]
        ) -> "OutboundTransfer":
            """
            Transitions a test mode created OutboundTransfer to the failed status. The OutboundTransfer must already be in the processing state.
            """
            return cast(
                "OutboundTransfer",
                await self.resource._request_async(
                    "post",
                    "/v1/test_helpers/treasury/outbound_transfers/{outbound_transfer}/fail".format(
                        outbound_transfer=sanitize_id(self.resource.get("id"))
                    ),
                    params=params,
                ),
            )

        @classmethod
        def _cls_post(
            cls,
            outbound_transfer: str,
            **params: Unpack["OutboundTransfer.PostParams"],
        ) -> "OutboundTransfer":
            """
            Transitions a test mode created OutboundTransfer to the posted status. The OutboundTransfer must already be in the processing state.
            """
            return cast(
                "OutboundTransfer",
                cls._static_request(
                    "post",
                    "/v1/test_helpers/treasury/outbound_transfers/{outbound_transfer}/post".format(
                        outbound_transfer=sanitize_id(outbound_transfer)
                    ),
                    params=params,
                ),
            )

        @overload
        @staticmethod
        def post(
            outbound_transfer: str,
            **params: Unpack["OutboundTransfer.PostParams"],
        ) -> "OutboundTransfer":
            """
            Transitions a test mode created OutboundTransfer to the posted status. The OutboundTransfer must already be in the processing state.
            """
            ...

        @overload
        def post(
            self, **params: Unpack["OutboundTransfer.PostParams"]
        ) -> "OutboundTransfer":
            """
            Transitions a test mode created OutboundTransfer to the posted status. The OutboundTransfer must already be in the processing state.
            """
            ...

        @class_method_variant("_cls_post")
        def post(  # pyright: ignore[reportGeneralTypeIssues]
            self, **params: Unpack["OutboundTransfer.PostParams"]
        ) -> "OutboundTransfer":
            """
            Transitions a test mode created OutboundTransfer to the posted status. The OutboundTransfer must already be in the processing state.
            """
            return cast(
                "OutboundTransfer",
                self.resource._request(
                    "post",
                    "/v1/test_helpers/treasury/outbound_transfers/{outbound_transfer}/post".format(
                        outbound_transfer=sanitize_id(self.resource.get("id"))
                    ),
                    params=params,
                ),
            )

        @classmethod
        async def _cls_post_async(
            cls,
            outbound_transfer: str,
            **params: Unpack["OutboundTransfer.PostParams"],
        ) -> "OutboundTransfer":
            """
            Transitions a test mode created OutboundTransfer to the posted status. The OutboundTransfer must already be in the processing state.
            """
            return cast(
                "OutboundTransfer",
                await cls._static_request_async(
                    "post",
                    "/v1/test_helpers/treasury/outbound_transfers/{outbound_transfer}/post".format(
                        outbound_transfer=sanitize_id(outbound_transfer)
                    ),
                    params=params,
                ),
            )

        @overload
        @staticmethod
        async def post_async(
            outbound_transfer: str,
            **params: Unpack["OutboundTransfer.PostParams"],
        ) -> "OutboundTransfer":
            """
            Transitions a test mode created OutboundTransfer to the posted status. The OutboundTransfer must already be in the processing state.
            """
            ...

        @overload
        async def post_async(
            self, **params: Unpack["OutboundTransfer.PostParams"]
        ) -> "OutboundTransfer":
            """
            Transitions a test mode created OutboundTransfer to the posted status. The OutboundTransfer must already be in the processing state.
            """
            ...

        @class_method_variant("_cls_post_async")
        async def post_async(  # pyright: ignore[reportGeneralTypeIssues]
            self, **params: Unpack["OutboundTransfer.PostParams"]
        ) -> "OutboundTransfer":
            """
            Transitions a test mode created OutboundTransfer to the posted status. The OutboundTransfer must already be in the processing state.
            """
            return cast(
                "OutboundTransfer",
                await self.resource._request_async(
                    "post",
                    "/v1/test_helpers/treasury/outbound_transfers/{outbound_transfer}/post".format(
                        outbound_transfer=sanitize_id(self.resource.get("id"))
                    ),
                    params=params,
                ),
            )

        @classmethod
        def _cls_return_outbound_transfer(
            cls,
            outbound_transfer: str,
            **params: Unpack["OutboundTransfer.ReturnOutboundTransferParams"],
        ) -> "OutboundTransfer":
            """
            Transitions a test mode created OutboundTransfer to the returned status. The OutboundTransfer must already be in the processing state.
            """
            return cast(
                "OutboundTransfer",
                cls._static_request(
                    "post",
                    "/v1/test_helpers/treasury/outbound_transfers/{outbound_transfer}/return".format(
                        outbound_transfer=sanitize_id(outbound_transfer)
                    ),
                    params=params,
                ),
            )

        @overload
        @staticmethod
        def return_outbound_transfer(
            outbound_transfer: str,
            **params: Unpack["OutboundTransfer.ReturnOutboundTransferParams"],
        ) -> "OutboundTransfer":
            """
            Transitions a test mode created OutboundTransfer to the returned status. The OutboundTransfer must already be in the processing state.
            """
            ...

        @overload
        def return_outbound_transfer(
            self,
            **params: Unpack["OutboundTransfer.ReturnOutboundTransferParams"],
        ) -> "OutboundTransfer":
            """
            Transitions a test mode created OutboundTransfer to the returned status. The OutboundTransfer must already be in the processing state.
            """
            ...

        @class_method_variant("_cls_return_outbound_transfer")
        def return_outbound_transfer(  # pyright: ignore[reportGeneralTypeIssues]
            self,
            **params: Unpack["OutboundTransfer.ReturnOutboundTransferParams"],
        ) -> "OutboundTransfer":
            """
            Transitions a test mode created OutboundTransfer to the returned status. The OutboundTransfer must already be in the processing state.
            """
            return cast(
                "OutboundTransfer",
                self.resource._request(
                    "post",
                    "/v1/test_helpers/treasury/outbound_transfers/{outbound_transfer}/return".format(
                        outbound_transfer=sanitize_id(self.resource.get("id"))
                    ),
                    params=params,
                ),
            )

        @classmethod
        async def _cls_return_outbound_transfer_async(
            cls,
            outbound_transfer: str,
            **params: Unpack["OutboundTransfer.ReturnOutboundTransferParams"],
        ) -> "OutboundTransfer":
            """
            Transitions a test mode created OutboundTransfer to the returned status. The OutboundTransfer must already be in the processing state.
            """
            return cast(
                "OutboundTransfer",
                await cls._static_request_async(
                    "post",
                    "/v1/test_helpers/treasury/outbound_transfers/{outbound_transfer}/return".format(
                        outbound_transfer=sanitize_id(outbound_transfer)
                    ),
                    params=params,
                ),
            )

        @overload
        @staticmethod
        async def return_outbound_transfer_async(
            outbound_transfer: str,
            **params: Unpack["OutboundTransfer.ReturnOutboundTransferParams"],
        ) -> "OutboundTransfer":
            """
            Transitions a test mode created OutboundTransfer to the returned status. The OutboundTransfer must already be in the processing state.
            """
            ...

        @overload
        async def return_outbound_transfer_async(
            self,
            **params: Unpack["OutboundTransfer.ReturnOutboundTransferParams"],
        ) -> "OutboundTransfer":
            """
            Transitions a test mode created OutboundTransfer to the returned status. The OutboundTransfer must already be in the processing state.
            """
            ...

        @class_method_variant("_cls_return_outbound_transfer_async")
        async def return_outbound_transfer_async(  # pyright: ignore[reportGeneralTypeIssues]
            self,
            **params: Unpack["OutboundTransfer.ReturnOutboundTransferParams"],
        ) -> "OutboundTransfer":
            """
            Transitions a test mode created OutboundTransfer to the returned status. The OutboundTransfer must already be in the processing state.
            """
            return cast(
                "OutboundTransfer",
                await self.resource._request_async(
                    "post",
                    "/v1/test_helpers/treasury/outbound_transfers/{outbound_transfer}/return".format(
                        outbound_transfer=sanitize_id(self.resource.get("id"))
                    ),
                    params=params,
                ),
            )

        @classmethod
        def _cls_update(
            cls,
            outbound_transfer: str,
            **params: Unpack["OutboundTransfer.UpdateParams"],
        ) -> "OutboundTransfer":
            """
            Updates a test mode created OutboundTransfer with tracking details. The OutboundTransfer must not be cancelable, and cannot be in the canceled or failed states.
            """
            return cast(
                "OutboundTransfer",
                cls._static_request(
                    "post",
                    "/v1/test_helpers/treasury/outbound_transfers/{outbound_transfer}".format(
                        outbound_transfer=sanitize_id(outbound_transfer)
                    ),
                    params=params,
                ),
            )

        @overload
        @staticmethod
        def update(
            outbound_transfer: str,
            **params: Unpack["OutboundTransfer.UpdateParams"],
        ) -> "OutboundTransfer":
            """
            Updates a test mode created OutboundTransfer with tracking details. The OutboundTransfer must not be cancelable, and cannot be in the canceled or failed states.
            """
            ...

        @overload
        def update(
            self, **params: Unpack["OutboundTransfer.UpdateParams"]
        ) -> "OutboundTransfer":
            """
            Updates a test mode created OutboundTransfer with tracking details. The OutboundTransfer must not be cancelable, and cannot be in the canceled or failed states.
            """
            ...

        @class_method_variant("_cls_update")
        def update(  # pyright: ignore[reportGeneralTypeIssues]
            self, **params: Unpack["OutboundTransfer.UpdateParams"]
        ) -> "OutboundTransfer":
            """
            Updates a test mode created OutboundTransfer with tracking details. The OutboundTransfer must not be cancelable, and cannot be in the canceled or failed states.
            """
            return cast(
                "OutboundTransfer",
                self.resource._request(
                    "post",
                    "/v1/test_helpers/treasury/outbound_transfers/{outbound_transfer}".format(
                        outbound_transfer=sanitize_id(self.resource.get("id"))
                    ),
                    params=params,
                ),
            )

        @classmethod
        async def _cls_update_async(
            cls,
            outbound_transfer: str,
            **params: Unpack["OutboundTransfer.UpdateParams"],
        ) -> "OutboundTransfer":
            """
            Updates a test mode created OutboundTransfer with tracking details. The OutboundTransfer must not be cancelable, and cannot be in the canceled or failed states.
            """
            return cast(
                "OutboundTransfer",
                await cls._static_request_async(
                    "post",
                    "/v1/test_helpers/treasury/outbound_transfers/{outbound_transfer}".format(
                        outbound_transfer=sanitize_id(outbound_transfer)
                    ),
                    params=params,
                ),
            )

        @overload
        @staticmethod
        async def update_async(
            outbound_transfer: str,
            **params: Unpack["OutboundTransfer.UpdateParams"],
        ) -> "OutboundTransfer":
            """
            Updates a test mode created OutboundTransfer with tracking details. The OutboundTransfer must not be cancelable, and cannot be in the canceled or failed states.
            """
            ...

        @overload
        async def update_async(
            self, **params: Unpack["OutboundTransfer.UpdateParams"]
        ) -> "OutboundTransfer":
            """
            Updates a test mode created OutboundTransfer with tracking details. The OutboundTransfer must not be cancelable, and cannot be in the canceled or failed states.
            """
            ...

        @class_method_variant("_cls_update_async")
        async def update_async(  # pyright: ignore[reportGeneralTypeIssues]
            self, **params: Unpack["OutboundTransfer.UpdateParams"]
        ) -> "OutboundTransfer":
            """
            Updates a test mode created OutboundTransfer with tracking details. The OutboundTransfer must not be cancelable, and cannot be in the canceled or failed states.
            """
            return cast(
                "OutboundTransfer",
                await self.resource._request_async(
                    "post",
                    "/v1/test_helpers/treasury/outbound_transfers/{outbound_transfer}".format(
                        outbound_transfer=sanitize_id(self.resource.get("id"))
                    ),
                    params=params,
                ),
            )

    @property
    def test_helpers(self):
        return self.TestHelpers(self)

    _inner_class_types = {
        "destination_payment_method_details": DestinationPaymentMethodDetails,
        "returned_details": ReturnedDetails,
        "status_transitions": StatusTransitions,
        "tracking_details": TrackingDetails,
    }


OutboundTransfer.TestHelpers._resource_cls = OutboundTransfer

# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._expandable_field import ExpandableField
from stripe._list_object import ListObject
from stripe._listable_api_resource import ListableAPIResource
from stripe._request_options import RequestOptions
from stripe._stripe_object import StripeObject
from stripe._updateable_api_resource import UpdateableAPIResource
from stripe._util import class_method_variant, sanitize_id
from typing import ClassVar, Dict, List, Optional, cast, overload
from typing_extensions import (
    Literal,
    NotRequired,
    TypedDict,
    Unpack,
    TYPE_CHECKING,
)

if TYPE_CHECKING:
    from stripe._balance_transaction import BalanceTransaction
    from stripe._charge import Charge
    from stripe._file import File
    from stripe._payment_intent import PaymentIntent


class Dispute(
    ListableAPIResource["Dispute"], UpdateableAPIResource["Dispute"]
):
    """
    A dispute occurs when a customer questions your charge with their card issuer.
    When this happens, you have the opportunity to respond to the dispute with
    evidence that shows that the charge is legitimate.

    Related guide: [Disputes and fraud](https://stripe.com/docs/disputes)
    """

    OBJECT_NAME: ClassVar[Literal["dispute"]] = "dispute"

    class Evidence(StripeObject):
        access_activity_log: Optional[str]
        """
        Any server or activity logs showing proof that the customer accessed or downloaded the purchased digital product. This information should include IP addresses, corresponding timestamps, and any detailed recorded activity.
        """
        billing_address: Optional[str]
        """
        The billing address provided by the customer.
        """
        cancellation_policy: Optional[ExpandableField["File"]]
        """
        (ID of a [file upload](https://stripe.com/docs/guides/file-upload)) Your subscription cancellation policy, as shown to the customer.
        """
        cancellation_policy_disclosure: Optional[str]
        """
        An explanation of how and when the customer was shown your refund policy prior to purchase.
        """
        cancellation_rebuttal: Optional[str]
        """
        A justification for why the customer's subscription was not canceled.
        """
        customer_communication: Optional[ExpandableField["File"]]
        """
        (ID of a [file upload](https://stripe.com/docs/guides/file-upload)) Any communication with the customer that you feel is relevant to your case. Examples include emails proving that the customer received the product or service, or demonstrating their use of or satisfaction with the product or service.
        """
        customer_email_address: Optional[str]
        """
        The email address of the customer.
        """
        customer_name: Optional[str]
        """
        The name of the customer.
        """
        customer_purchase_ip: Optional[str]
        """
        The IP address that the customer used when making the purchase.
        """
        customer_signature: Optional[ExpandableField["File"]]
        """
        (ID of a [file upload](https://stripe.com/docs/guides/file-upload)) A relevant document or contract showing the customer's signature.
        """
        duplicate_charge_documentation: Optional[ExpandableField["File"]]
        """
        (ID of a [file upload](https://stripe.com/docs/guides/file-upload)) Documentation for the prior charge that can uniquely identify the charge, such as a receipt, shipping label, work order, etc. This document should be paired with a similar document from the disputed payment that proves the two payments are separate.
        """
        duplicate_charge_explanation: Optional[str]
        """
        An explanation of the difference between the disputed charge versus the prior charge that appears to be a duplicate.
        """
        duplicate_charge_id: Optional[str]
        """
        The Stripe ID for the prior charge which appears to be a duplicate of the disputed charge.
        """
        product_description: Optional[str]
        """
        A description of the product or service that was sold.
        """
        receipt: Optional[ExpandableField["File"]]
        """
        (ID of a [file upload](https://stripe.com/docs/guides/file-upload)) Any receipt or message sent to the customer notifying them of the charge.
        """
        refund_policy: Optional[ExpandableField["File"]]
        """
        (ID of a [file upload](https://stripe.com/docs/guides/file-upload)) Your refund policy, as shown to the customer.
        """
        refund_policy_disclosure: Optional[str]
        """
        Documentation demonstrating that the customer was shown your refund policy prior to purchase.
        """
        refund_refusal_explanation: Optional[str]
        """
        A justification for why the customer is not entitled to a refund.
        """
        service_date: Optional[str]
        """
        The date on which the customer received or began receiving the purchased service, in a clear human-readable format.
        """
        service_documentation: Optional[ExpandableField["File"]]
        """
        (ID of a [file upload](https://stripe.com/docs/guides/file-upload)) Documentation showing proof that a service was provided to the customer. This could include a copy of a signed contract, work order, or other form of written agreement.
        """
        shipping_address: Optional[str]
        """
        The address to which a physical product was shipped. You should try to include as complete address information as possible.
        """
        shipping_carrier: Optional[str]
        """
        The delivery service that shipped a physical product, such as Fedex, UPS, USPS, etc. If multiple carriers were used for this purchase, please separate them with commas.
        """
        shipping_date: Optional[str]
        """
        The date on which a physical product began its route to the shipping address, in a clear human-readable format.
        """
        shipping_documentation: Optional[ExpandableField["File"]]
        """
        (ID of a [file upload](https://stripe.com/docs/guides/file-upload)) Documentation showing proof that a product was shipped to the customer at the same address the customer provided to you. This could include a copy of the shipment receipt, shipping label, etc. It should show the customer's full shipping address, if possible.
        """
        shipping_tracking_number: Optional[str]
        """
        The tracking number for a physical product, obtained from the delivery service. If multiple tracking numbers were generated for this purchase, please separate them with commas.
        """
        uncategorized_file: Optional[ExpandableField["File"]]
        """
        (ID of a [file upload](https://stripe.com/docs/guides/file-upload)) Any additional evidence or statements.
        """
        uncategorized_text: Optional[str]
        """
        Any additional evidence or statements.
        """

    class EvidenceDetails(StripeObject):
        due_by: Optional[int]
        """
        Date by which evidence must be submitted in order to successfully challenge dispute. Will be 0 if the customer's bank or credit card company doesn't allow a response for this particular dispute.
        """
        has_evidence: bool
        """
        Whether evidence has been staged for this dispute.
        """
        past_due: bool
        """
        Whether the last evidence submission was submitted past the due date. Defaults to `false` if no evidence submissions have occurred. If `true`, then delivery of the latest evidence is *not* guaranteed.
        """
        submission_count: int
        """
        The number of times evidence has been submitted. Typically, you may only submit evidence once.
        """

    class PaymentMethodDetails(StripeObject):
        class Card(StripeObject):
            brand: str
            """
            Card brand. Can be `amex`, `diners`, `discover`, `eftpos_au`, `jcb`, `mastercard`, `unionpay`, `visa`, or `unknown`.
            """
            case_type: Literal["chargeback", "inquiry"]
            """
            The type of dispute opened. Different case types may have varying fees and financial impact.
            """
            network_reason_code: Optional[str]
            """
            The card network's specific dispute reason code, which maps to one of Stripe's primary dispute categories to simplify response guidance. The [Network code map](https://stripe.com/docs/disputes/categories#network-code-map) lists all available dispute reason codes by network.
            """

        class Klarna(StripeObject):
            reason_code: Optional[str]
            """
            The reason for the dispute as defined by Klarna
            """

        class Paypal(StripeObject):
            case_id: Optional[str]
            """
            The ID of the dispute in PayPal.
            """
            reason_code: Optional[str]
            """
            The reason for the dispute as defined by PayPal
            """

        card: Optional[Card]
        klarna: Optional[Klarna]
        paypal: Optional[Paypal]
        type: Literal["card", "klarna", "paypal"]
        """
        Payment method type.
        """
        _inner_class_types = {"card": Card, "klarna": Klarna, "paypal": Paypal}

    class CloseParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class ListParams(RequestOptions):
        charge: NotRequired[str]
        """
        Only return disputes associated to the charge specified by this charge ID.
        """
        created: NotRequired["Dispute.ListParamsCreated|int"]
        """
        Only return disputes that were created during the given date interval.
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
        Only return disputes associated to the PaymentIntent specified by this PaymentIntent ID.
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

    class ModifyParams(RequestOptions):
        evidence: NotRequired["Dispute.ModifyParamsEvidence"]
        """
        Evidence to upload, to respond to a dispute. Updating any field in the hash will submit all fields in the hash for review. The combined character count of all fields is limited to 150,000.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        submit: NotRequired[bool]
        """
        Whether to immediately submit evidence to the bank. If `false`, evidence is staged on the dispute. Staged evidence is visible in the API and Dashboard, and can be submitted to the bank by making another request with this attribute set to `true` (the default).
        """

    class ModifyParamsEvidence(TypedDict):
        access_activity_log: NotRequired[str]
        """
        Any server or activity logs showing proof that the customer accessed or downloaded the purchased digital product. This information should include IP addresses, corresponding timestamps, and any detailed recorded activity. Has a maximum character count of 20,000.
        """
        billing_address: NotRequired[str]
        """
        The billing address provided by the customer.
        """
        cancellation_policy: NotRequired[str]
        """
        (ID of a [file upload](https://stripe.com/docs/guides/file-upload)) Your subscription cancellation policy, as shown to the customer.
        """
        cancellation_policy_disclosure: NotRequired[str]
        """
        An explanation of how and when the customer was shown your refund policy prior to purchase. Has a maximum character count of 20,000.
        """
        cancellation_rebuttal: NotRequired[str]
        """
        A justification for why the customer's subscription was not canceled. Has a maximum character count of 20,000.
        """
        customer_communication: NotRequired[str]
        """
        (ID of a [file upload](https://stripe.com/docs/guides/file-upload)) Any communication with the customer that you feel is relevant to your case. Examples include emails proving that the customer received the product or service, or demonstrating their use of or satisfaction with the product or service.
        """
        customer_email_address: NotRequired[str]
        """
        The email address of the customer.
        """
        customer_name: NotRequired[str]
        """
        The name of the customer.
        """
        customer_purchase_ip: NotRequired[str]
        """
        The IP address that the customer used when making the purchase.
        """
        customer_signature: NotRequired[str]
        """
        (ID of a [file upload](https://stripe.com/docs/guides/file-upload)) A relevant document or contract showing the customer's signature.
        """
        duplicate_charge_documentation: NotRequired[str]
        """
        (ID of a [file upload](https://stripe.com/docs/guides/file-upload)) Documentation for the prior charge that can uniquely identify the charge, such as a receipt, shipping label, work order, etc. This document should be paired with a similar document from the disputed payment that proves the two payments are separate.
        """
        duplicate_charge_explanation: NotRequired[str]
        """
        An explanation of the difference between the disputed charge versus the prior charge that appears to be a duplicate. Has a maximum character count of 20,000.
        """
        duplicate_charge_id: NotRequired[str]
        """
        The Stripe ID for the prior charge which appears to be a duplicate of the disputed charge.
        """
        product_description: NotRequired[str]
        """
        A description of the product or service that was sold. Has a maximum character count of 20,000.
        """
        receipt: NotRequired[str]
        """
        (ID of a [file upload](https://stripe.com/docs/guides/file-upload)) Any receipt or message sent to the customer notifying them of the charge.
        """
        refund_policy: NotRequired[str]
        """
        (ID of a [file upload](https://stripe.com/docs/guides/file-upload)) Your refund policy, as shown to the customer.
        """
        refund_policy_disclosure: NotRequired[str]
        """
        Documentation demonstrating that the customer was shown your refund policy prior to purchase. Has a maximum character count of 20,000.
        """
        refund_refusal_explanation: NotRequired[str]
        """
        A justification for why the customer is not entitled to a refund. Has a maximum character count of 20,000.
        """
        service_date: NotRequired[str]
        """
        The date on which the customer received or began receiving the purchased service, in a clear human-readable format.
        """
        service_documentation: NotRequired[str]
        """
        (ID of a [file upload](https://stripe.com/docs/guides/file-upload)) Documentation showing proof that a service was provided to the customer. This could include a copy of a signed contract, work order, or other form of written agreement.
        """
        shipping_address: NotRequired[str]
        """
        The address to which a physical product was shipped. You should try to include as complete address information as possible.
        """
        shipping_carrier: NotRequired[str]
        """
        The delivery service that shipped a physical product, such as Fedex, UPS, USPS, etc. If multiple carriers were used for this purchase, please separate them with commas.
        """
        shipping_date: NotRequired[str]
        """
        The date on which a physical product began its route to the shipping address, in a clear human-readable format.
        """
        shipping_documentation: NotRequired[str]
        """
        (ID of a [file upload](https://stripe.com/docs/guides/file-upload)) Documentation showing proof that a product was shipped to the customer at the same address the customer provided to you. This could include a copy of the shipment receipt, shipping label, etc. It should show the customer's full shipping address, if possible.
        """
        shipping_tracking_number: NotRequired[str]
        """
        The tracking number for a physical product, obtained from the delivery service. If multiple tracking numbers were generated for this purchase, please separate them with commas.
        """
        uncategorized_file: NotRequired[str]
        """
        (ID of a [file upload](https://stripe.com/docs/guides/file-upload)) Any additional evidence or statements.
        """
        uncategorized_text: NotRequired[str]
        """
        Any additional evidence or statements. Has a maximum character count of 20,000.
        """

    class RetrieveParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    amount: int
    """
    Disputed amount. Usually the amount of the charge, but it can differ (usually because of currency fluctuation or because only part of the order is disputed).
    """
    balance_transactions: List["BalanceTransaction"]
    """
    List of zero, one, or two balance transactions that show funds withdrawn and reinstated to your Stripe account as a result of this dispute.
    """
    charge: ExpandableField["Charge"]
    """
    ID of the charge that's disputed.
    """
    created: int
    """
    Time at which the object was created. Measured in seconds since the Unix epoch.
    """
    currency: str
    """
    Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
    """
    evidence: Evidence
    evidence_details: EvidenceDetails
    id: str
    """
    Unique identifier for the object.
    """
    is_charge_refundable: bool
    """
    If true, it's still possible to refund the disputed payment. After the payment has been fully refunded, no further funds are withdrawn from your Stripe account as a result of this dispute.
    """
    livemode: bool
    """
    Has the value `true` if the object exists in live mode or the value `false` if the object exists in test mode.
    """
    metadata: Dict[str, str]
    """
    Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format.
    """
    network_reason_code: Optional[str]
    """
    Network-dependent reason code for the dispute.
    """
    object: Literal["dispute"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """
    payment_intent: Optional[ExpandableField["PaymentIntent"]]
    """
    ID of the PaymentIntent that's disputed.
    """
    payment_method_details: Optional[PaymentMethodDetails]
    reason: str
    """
    Reason given by cardholder for dispute. Possible values are `bank_cannot_process`, `check_returned`, `credit_not_processed`, `customer_initiated`, `debit_not_authorized`, `duplicate`, `fraudulent`, `general`, `incorrect_account_details`, `insufficient_funds`, `product_not_received`, `product_unacceptable`, `subscription_canceled`, or `unrecognized`. Learn more about [dispute reasons](https://stripe.com/docs/disputes/categories).
    """
    status: Literal[
        "lost",
        "needs_response",
        "under_review",
        "warning_closed",
        "warning_needs_response",
        "warning_under_review",
        "won",
    ]
    """
    Current status of dispute. Possible values are `warning_needs_response`, `warning_under_review`, `warning_closed`, `needs_response`, `under_review`, `won`, or `lost`.
    """

    @classmethod
    def _cls_close(
        cls, dispute: str, **params: Unpack["Dispute.CloseParams"]
    ) -> "Dispute":
        """
        Closing the dispute for a charge indicates that you do not have any evidence to submit and are essentially dismissing the dispute, acknowledging it as lost.

        The status of the dispute will change from needs_response to lost. Closing a dispute is irreversible.
        """
        return cast(
            "Dispute",
            cls._static_request(
                "post",
                "/v1/disputes/{dispute}/close".format(
                    dispute=sanitize_id(dispute)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    def close(
        dispute: str, **params: Unpack["Dispute.CloseParams"]
    ) -> "Dispute":
        """
        Closing the dispute for a charge indicates that you do not have any evidence to submit and are essentially dismissing the dispute, acknowledging it as lost.

        The status of the dispute will change from needs_response to lost. Closing a dispute is irreversible.
        """
        ...

    @overload
    def close(self, **params: Unpack["Dispute.CloseParams"]) -> "Dispute":
        """
        Closing the dispute for a charge indicates that you do not have any evidence to submit and are essentially dismissing the dispute, acknowledging it as lost.

        The status of the dispute will change from needs_response to lost. Closing a dispute is irreversible.
        """
        ...

    @class_method_variant("_cls_close")
    def close(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Dispute.CloseParams"]
    ) -> "Dispute":
        """
        Closing the dispute for a charge indicates that you do not have any evidence to submit and are essentially dismissing the dispute, acknowledging it as lost.

        The status of the dispute will change from needs_response to lost. Closing a dispute is irreversible.
        """
        return cast(
            "Dispute",
            self._request(
                "post",
                "/v1/disputes/{dispute}/close".format(
                    dispute=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    async def _cls_close_async(
        cls, dispute: str, **params: Unpack["Dispute.CloseParams"]
    ) -> "Dispute":
        """
        Closing the dispute for a charge indicates that you do not have any evidence to submit and are essentially dismissing the dispute, acknowledging it as lost.

        The status of the dispute will change from needs_response to lost. Closing a dispute is irreversible.
        """
        return cast(
            "Dispute",
            await cls._static_request_async(
                "post",
                "/v1/disputes/{dispute}/close".format(
                    dispute=sanitize_id(dispute)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    async def close_async(
        dispute: str, **params: Unpack["Dispute.CloseParams"]
    ) -> "Dispute":
        """
        Closing the dispute for a charge indicates that you do not have any evidence to submit and are essentially dismissing the dispute, acknowledging it as lost.

        The status of the dispute will change from needs_response to lost. Closing a dispute is irreversible.
        """
        ...

    @overload
    async def close_async(
        self, **params: Unpack["Dispute.CloseParams"]
    ) -> "Dispute":
        """
        Closing the dispute for a charge indicates that you do not have any evidence to submit and are essentially dismissing the dispute, acknowledging it as lost.

        The status of the dispute will change from needs_response to lost. Closing a dispute is irreversible.
        """
        ...

    @class_method_variant("_cls_close_async")
    async def close_async(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Dispute.CloseParams"]
    ) -> "Dispute":
        """
        Closing the dispute for a charge indicates that you do not have any evidence to submit and are essentially dismissing the dispute, acknowledging it as lost.

        The status of the dispute will change from needs_response to lost. Closing a dispute is irreversible.
        """
        return cast(
            "Dispute",
            await self._request_async(
                "post",
                "/v1/disputes/{dispute}/close".format(
                    dispute=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    def list(
        cls, **params: Unpack["Dispute.ListParams"]
    ) -> ListObject["Dispute"]:
        """
        Returns a list of your disputes.
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
        cls, **params: Unpack["Dispute.ListParams"]
    ) -> ListObject["Dispute"]:
        """
        Returns a list of your disputes.
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
        cls, id: str, **params: Unpack["Dispute.ModifyParams"]
    ) -> "Dispute":
        """
        When you get a dispute, contacting your customer is always the best first step. If that doesn't work, you can submit evidence to help us resolve the dispute in your favor. You can do this in your [dashboard](https://dashboard.stripe.com/disputes), but if you prefer, you can use the API to submit evidence programmatically.

        Depending on your dispute type, different evidence fields will give you a better chance of winning your dispute. To figure out which evidence fields to provide, see our [guide to dispute types](https://stripe.com/docs/disputes/categories).
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(id))
        return cast(
            "Dispute",
            cls._static_request(
                "post",
                url,
                params=params,
            ),
        )

    @classmethod
    async def modify_async(
        cls, id: str, **params: Unpack["Dispute.ModifyParams"]
    ) -> "Dispute":
        """
        When you get a dispute, contacting your customer is always the best first step. If that doesn't work, you can submit evidence to help us resolve the dispute in your favor. You can do this in your [dashboard](https://dashboard.stripe.com/disputes), but if you prefer, you can use the API to submit evidence programmatically.

        Depending on your dispute type, different evidence fields will give you a better chance of winning your dispute. To figure out which evidence fields to provide, see our [guide to dispute types](https://stripe.com/docs/disputes/categories).
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(id))
        return cast(
            "Dispute",
            await cls._static_request_async(
                "post",
                url,
                params=params,
            ),
        )

    @classmethod
    def retrieve(
        cls, id: str, **params: Unpack["Dispute.RetrieveParams"]
    ) -> "Dispute":
        """
        Retrieves the dispute with the given ID.
        """
        instance = cls(id, **params)
        instance.refresh()
        return instance

    @classmethod
    async def retrieve_async(
        cls, id: str, **params: Unpack["Dispute.RetrieveParams"]
    ) -> "Dispute":
        """
        Retrieves the dispute with the given ID.
        """
        instance = cls(id, **params)
        await instance.refresh_async()
        return instance

    _inner_class_types = {
        "evidence": Evidence,
        "evidence_details": EvidenceDetails,
        "payment_method_details": PaymentMethodDetails,
    }

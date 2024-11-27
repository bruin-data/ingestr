# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._createable_api_resource import CreateableAPIResource
from stripe._customer import Customer
from stripe._error import InvalidRequestError
from stripe._list_object import ListObject
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
    from stripe._source_transaction import SourceTransaction


class Source(CreateableAPIResource["Source"], UpdateableAPIResource["Source"]):
    """
    `Source` objects allow you to accept a variety of payment methods. They
    represent a customer's payment instrument, and can be used with the Stripe API
    just like a `Card` object: once chargeable, they can be charged, or can be
    attached to customers.

    Stripe doesn't recommend using the deprecated [Sources API](https://stripe.com/docs/api/sources).
    We recommend that you adopt the [PaymentMethods API](https://stripe.com/docs/api/payment_methods).
    This newer API provides access to our latest features and payment method types.

    Related guides: [Sources API](https://stripe.com/docs/sources) and [Sources & Customers](https://stripe.com/docs/sources/customers).
    """

    OBJECT_NAME: ClassVar[Literal["source"]] = "source"

    class AchCreditTransfer(StripeObject):
        account_number: Optional[str]
        bank_name: Optional[str]
        fingerprint: Optional[str]
        refund_account_holder_name: Optional[str]
        refund_account_holder_type: Optional[str]
        refund_routing_number: Optional[str]
        routing_number: Optional[str]
        swift_code: Optional[str]

    class AchDebit(StripeObject):
        bank_name: Optional[str]
        country: Optional[str]
        fingerprint: Optional[str]
        last4: Optional[str]
        routing_number: Optional[str]
        type: Optional[str]

    class AcssDebit(StripeObject):
        bank_address_city: Optional[str]
        bank_address_line_1: Optional[str]
        bank_address_line_2: Optional[str]
        bank_address_postal_code: Optional[str]
        bank_name: Optional[str]
        category: Optional[str]
        country: Optional[str]
        fingerprint: Optional[str]
        last4: Optional[str]
        routing_number: Optional[str]

    class Alipay(StripeObject):
        data_string: Optional[str]
        native_url: Optional[str]
        statement_descriptor: Optional[str]

    class AuBecsDebit(StripeObject):
        bsb_number: Optional[str]
        fingerprint: Optional[str]
        last4: Optional[str]

    class Bancontact(StripeObject):
        bank_code: Optional[str]
        bank_name: Optional[str]
        bic: Optional[str]
        iban_last4: Optional[str]
        preferred_language: Optional[str]
        statement_descriptor: Optional[str]

    class Card(StripeObject):
        address_line1_check: Optional[str]
        address_zip_check: Optional[str]
        brand: Optional[str]
        country: Optional[str]
        cvc_check: Optional[str]
        description: Optional[str]
        dynamic_last4: Optional[str]
        exp_month: Optional[int]
        exp_year: Optional[int]
        fingerprint: Optional[str]
        funding: Optional[str]
        iin: Optional[str]
        issuer: Optional[str]
        last4: Optional[str]
        name: Optional[str]
        three_d_secure: Optional[str]
        tokenization_method: Optional[str]

    class CardPresent(StripeObject):
        application_cryptogram: Optional[str]
        application_preferred_name: Optional[str]
        authorization_code: Optional[str]
        authorization_response_code: Optional[str]
        brand: Optional[str]
        country: Optional[str]
        cvm_type: Optional[str]
        data_type: Optional[str]
        dedicated_file_name: Optional[str]
        description: Optional[str]
        emv_auth_data: Optional[str]
        evidence_customer_signature: Optional[str]
        evidence_transaction_certificate: Optional[str]
        exp_month: Optional[int]
        exp_year: Optional[int]
        fingerprint: Optional[str]
        funding: Optional[str]
        iin: Optional[str]
        issuer: Optional[str]
        last4: Optional[str]
        pos_device_id: Optional[str]
        pos_entry_mode: Optional[str]
        read_method: Optional[str]
        reader: Optional[str]
        terminal_verification_results: Optional[str]
        transaction_status_information: Optional[str]

    class CodeVerification(StripeObject):
        attempts_remaining: int
        """
        The number of attempts remaining to authenticate the source object with a verification code.
        """
        status: str
        """
        The status of the code verification, either `pending` (awaiting verification, `attempts_remaining` should be greater than 0), `succeeded` (successful verification) or `failed` (failed verification, cannot be verified anymore as `attempts_remaining` should be 0).
        """

    class Eps(StripeObject):
        reference: Optional[str]
        statement_descriptor: Optional[str]

    class Giropay(StripeObject):
        bank_code: Optional[str]
        bank_name: Optional[str]
        bic: Optional[str]
        statement_descriptor: Optional[str]

    class Ideal(StripeObject):
        bank: Optional[str]
        bic: Optional[str]
        iban_last4: Optional[str]
        statement_descriptor: Optional[str]

    class Klarna(StripeObject):
        background_image_url: Optional[str]
        client_token: Optional[str]
        first_name: Optional[str]
        last_name: Optional[str]
        locale: Optional[str]
        logo_url: Optional[str]
        page_title: Optional[str]
        pay_later_asset_urls_descriptive: Optional[str]
        pay_later_asset_urls_standard: Optional[str]
        pay_later_name: Optional[str]
        pay_later_redirect_url: Optional[str]
        pay_now_asset_urls_descriptive: Optional[str]
        pay_now_asset_urls_standard: Optional[str]
        pay_now_name: Optional[str]
        pay_now_redirect_url: Optional[str]
        pay_over_time_asset_urls_descriptive: Optional[str]
        pay_over_time_asset_urls_standard: Optional[str]
        pay_over_time_name: Optional[str]
        pay_over_time_redirect_url: Optional[str]
        payment_method_categories: Optional[str]
        purchase_country: Optional[str]
        purchase_type: Optional[str]
        redirect_url: Optional[str]
        shipping_delay: Optional[int]
        shipping_first_name: Optional[str]
        shipping_last_name: Optional[str]

    class Multibanco(StripeObject):
        entity: Optional[str]
        reference: Optional[str]
        refund_account_holder_address_city: Optional[str]
        refund_account_holder_address_country: Optional[str]
        refund_account_holder_address_line1: Optional[str]
        refund_account_holder_address_line2: Optional[str]
        refund_account_holder_address_postal_code: Optional[str]
        refund_account_holder_address_state: Optional[str]
        refund_account_holder_name: Optional[str]
        refund_iban: Optional[str]

    class Owner(StripeObject):
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

        class VerifiedAddress(StripeObject):
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

        address: Optional[Address]
        """
        Owner's address.
        """
        email: Optional[str]
        """
        Owner's email address.
        """
        name: Optional[str]
        """
        Owner's full name.
        """
        phone: Optional[str]
        """
        Owner's phone number (including extension).
        """
        verified_address: Optional[VerifiedAddress]
        """
        Verified owner's address. Verified values are verified or provided by the payment method directly (and if supported) at the time of authorization or settlement. They cannot be set or mutated.
        """
        verified_email: Optional[str]
        """
        Verified owner's email address. Verified values are verified or provided by the payment method directly (and if supported) at the time of authorization or settlement. They cannot be set or mutated.
        """
        verified_name: Optional[str]
        """
        Verified owner's full name. Verified values are verified or provided by the payment method directly (and if supported) at the time of authorization or settlement. They cannot be set or mutated.
        """
        verified_phone: Optional[str]
        """
        Verified owner's phone number (including extension). Verified values are verified or provided by the payment method directly (and if supported) at the time of authorization or settlement. They cannot be set or mutated.
        """
        _inner_class_types = {
            "address": Address,
            "verified_address": VerifiedAddress,
        }

    class P24(StripeObject):
        reference: Optional[str]

    class Receiver(StripeObject):
        address: Optional[str]
        """
        The address of the receiver source. This is the value that should be communicated to the customer to send their funds to.
        """
        amount_charged: int
        """
        The total amount that was moved to your balance. This is almost always equal to the amount charged. In rare cases when customers deposit excess funds and we are unable to refund those, those funds get moved to your balance and show up in amount_charged as well. The amount charged is expressed in the source's currency.
        """
        amount_received: int
        """
        The total amount received by the receiver source. `amount_received = amount_returned + amount_charged` should be true for consumed sources unless customers deposit excess funds. The amount received is expressed in the source's currency.
        """
        amount_returned: int
        """
        The total amount that was returned to the customer. The amount returned is expressed in the source's currency.
        """
        refund_attributes_method: str
        """
        Type of refund attribute method, one of `email`, `manual`, or `none`.
        """
        refund_attributes_status: str
        """
        Type of refund attribute status, one of `missing`, `requested`, or `available`.
        """

    class Redirect(StripeObject):
        failure_reason: Optional[str]
        """
        The failure reason for the redirect, either `user_abort` (the customer aborted or dropped out of the redirect flow), `declined` (the authentication failed or the transaction was declined), or `processing_error` (the redirect failed due to a technical error). Present only if the redirect status is `failed`.
        """
        return_url: str
        """
        The URL you provide to redirect the customer to after they authenticated their payment.
        """
        status: str
        """
        The status of the redirect, either `pending` (ready to be used by your customer to authenticate the transaction), `succeeded` (succesful authentication, cannot be reused) or `not_required` (redirect should not be used) or `failed` (failed authentication, cannot be reused).
        """
        url: str
        """
        The URL provided to you to redirect a customer to as part of a `redirect` authentication flow.
        """

    class SepaCreditTransfer(StripeObject):
        bank_name: Optional[str]
        bic: Optional[str]
        iban: Optional[str]
        refund_account_holder_address_city: Optional[str]
        refund_account_holder_address_country: Optional[str]
        refund_account_holder_address_line1: Optional[str]
        refund_account_holder_address_line2: Optional[str]
        refund_account_holder_address_postal_code: Optional[str]
        refund_account_holder_address_state: Optional[str]
        refund_account_holder_name: Optional[str]
        refund_iban: Optional[str]

    class SepaDebit(StripeObject):
        bank_code: Optional[str]
        branch_code: Optional[str]
        country: Optional[str]
        fingerprint: Optional[str]
        last4: Optional[str]
        mandate_reference: Optional[str]
        mandate_url: Optional[str]

    class Sofort(StripeObject):
        bank_code: Optional[str]
        bank_name: Optional[str]
        bic: Optional[str]
        country: Optional[str]
        iban_last4: Optional[str]
        preferred_language: Optional[str]
        statement_descriptor: Optional[str]

    class SourceOrder(StripeObject):
        class Item(StripeObject):
            amount: Optional[int]
            """
            The amount (price) for this order item.
            """
            currency: Optional[str]
            """
            This currency of this order item. Required when `amount` is present.
            """
            description: Optional[str]
            """
            Human-readable description for this order item.
            """
            parent: Optional[str]
            """
            The ID of the associated object for this line item. Expandable if not null (e.g., expandable to a SKU).
            """
            quantity: Optional[int]
            """
            The quantity of this order item. When type is `sku`, this is the number of instances of the SKU to be ordered.
            """
            type: Optional[str]
            """
            The type of this order item. Must be `sku`, `tax`, or `shipping`.
            """

        class Shipping(StripeObject):
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

            address: Optional[Address]
            carrier: Optional[str]
            """
            The delivery service that shipped a physical product, such as Fedex, UPS, USPS, etc.
            """
            name: Optional[str]
            """
            Recipient name.
            """
            phone: Optional[str]
            """
            Recipient phone (including extension).
            """
            tracking_number: Optional[str]
            """
            The tracking number for a physical product, obtained from the delivery service. If multiple tracking numbers were generated for this purchase, please separate them with commas.
            """
            _inner_class_types = {"address": Address}

        amount: int
        """
        A positive integer in the smallest currency unit (that is, 100 cents for $1.00, or 1 for ¥1, Japanese Yen being a zero-decimal currency) representing the total amount for the order.
        """
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        email: Optional[str]
        """
        The email address of the customer placing the order.
        """
        items: Optional[List[Item]]
        """
        List of items constituting the order.
        """
        shipping: Optional[Shipping]
        _inner_class_types = {"items": Item, "shipping": Shipping}

    class ThreeDSecure(StripeObject):
        address_line1_check: Optional[str]
        address_zip_check: Optional[str]
        authenticated: Optional[bool]
        brand: Optional[str]
        card: Optional[str]
        country: Optional[str]
        customer: Optional[str]
        cvc_check: Optional[str]
        description: Optional[str]
        dynamic_last4: Optional[str]
        exp_month: Optional[int]
        exp_year: Optional[int]
        fingerprint: Optional[str]
        funding: Optional[str]
        iin: Optional[str]
        issuer: Optional[str]
        last4: Optional[str]
        name: Optional[str]
        three_d_secure: Optional[str]
        tokenization_method: Optional[str]

    class Wechat(StripeObject):
        prepay_id: Optional[str]
        qr_code_url: Optional[str]
        statement_descriptor: Optional[str]

    class CreateParams(RequestOptions):
        amount: NotRequired[int]
        """
        Amount associated with the source. This is the amount for which the source will be chargeable once ready. Required for `single_use` sources. Not supported for `receiver` type sources, where charge amount may not be specified until funds land.
        """
        currency: NotRequired[str]
        """
        Three-letter [ISO code for the currency](https://stripe.com/docs/currencies) associated with the source. This is the currency for which the source will be chargeable once ready.
        """
        customer: NotRequired[str]
        """
        The `Customer` to whom the original source is attached to. Must be set when the original source is not a `Source` (e.g., `Card`).
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        flow: NotRequired[
            Literal["code_verification", "none", "receiver", "redirect"]
        ]
        """
        The authentication `flow` of the source to create. `flow` is one of `redirect`, `receiver`, `code_verification`, `none`. It is generally inferred unless a type supports multiple flows.
        """
        mandate: NotRequired["Source.CreateParamsMandate"]
        """
        Information about a mandate possibility attached to a source object (generally for bank debits) as well as its acceptance status.
        """
        metadata: NotRequired[Dict[str, str]]
        original_source: NotRequired[str]
        """
        The source to share.
        """
        owner: NotRequired["Source.CreateParamsOwner"]
        """
        Information about the owner of the payment instrument that may be used or required by particular source types.
        """
        receiver: NotRequired["Source.CreateParamsReceiver"]
        """
        Optional parameters for the receiver flow. Can be set only if the source is a receiver (`flow` is `receiver`).
        """
        redirect: NotRequired["Source.CreateParamsRedirect"]
        """
        Parameters required for the redirect flow. Required if the source is authenticated by a redirect (`flow` is `redirect`).
        """
        source_order: NotRequired["Source.CreateParamsSourceOrder"]
        """
        Information about the items and shipping associated with the source. Required for transactional credit (for example Klarna) sources before you can charge it.
        """
        statement_descriptor: NotRequired[str]
        """
        An arbitrary string to be displayed on your customer's statement. As an example, if your website is `RunClub` and the item you're charging for is a race ticket, you may want to specify a `statement_descriptor` of `RunClub 5K race ticket.` While many payment types will display this information, some may not display it at all.
        """
        token: NotRequired[str]
        """
        An optional token used to create the source. When passed, token properties will override source parameters.
        """
        type: NotRequired[str]
        """
        The `type` of the source to create. Required unless `customer` and `original_source` are specified (see the [Cloning card Sources](https://stripe.com/docs/sources/connect#cloning-card-sources) guide)
        """
        usage: NotRequired[Literal["reusable", "single_use"]]

    class CreateParamsMandate(TypedDict):
        acceptance: NotRequired["Source.CreateParamsMandateAcceptance"]
        """
        The parameters required to notify Stripe of a mandate acceptance or refusal by the customer.
        """
        amount: NotRequired["Literal['']|int"]
        """
        The amount specified by the mandate. (Leave null for a mandate covering all amounts)
        """
        currency: NotRequired[str]
        """
        The currency specified by the mandate. (Must match `currency` of the source)
        """
        interval: NotRequired[Literal["one_time", "scheduled", "variable"]]
        """
        The interval of debits permitted by the mandate. Either `one_time` (just permitting a single debit), `scheduled` (with debits on an agreed schedule or for clearly-defined events), or `variable`(for debits with any frequency)
        """
        notification_method: NotRequired[
            Literal[
                "deprecated_none", "email", "manual", "none", "stripe_email"
            ]
        ]
        """
        The method Stripe should use to notify the customer of upcoming debit instructions and/or mandate confirmation as required by the underlying debit network. Either `email` (an email is sent directly to the customer), `manual` (a `source.mandate_notification` event is sent to your webhooks endpoint and you should handle the notification) or `none` (the underlying debit network does not require any notification).
        """

    class CreateParamsMandateAcceptance(TypedDict):
        date: NotRequired[int]
        """
        The Unix timestamp (in seconds) when the mandate was accepted or refused by the customer.
        """
        ip: NotRequired[str]
        """
        The IP address from which the mandate was accepted or refused by the customer.
        """
        offline: NotRequired["Source.CreateParamsMandateAcceptanceOffline"]
        """
        The parameters required to store a mandate accepted offline. Should only be set if `mandate[type]` is `offline`
        """
        online: NotRequired["Source.CreateParamsMandateAcceptanceOnline"]
        """
        The parameters required to store a mandate accepted online. Should only be set if `mandate[type]` is `online`
        """
        status: Literal["accepted", "pending", "refused", "revoked"]
        """
        The status of the mandate acceptance. Either `accepted` (the mandate was accepted) or `refused` (the mandate was refused).
        """
        type: NotRequired[Literal["offline", "online"]]
        """
        The type of acceptance information included with the mandate. Either `online` or `offline`
        """
        user_agent: NotRequired[str]
        """
        The user agent of the browser from which the mandate was accepted or refused by the customer.
        """

    class CreateParamsMandateAcceptanceOffline(TypedDict):
        contact_email: str
        """
        An email to contact you with if a copy of the mandate is requested, required if `type` is `offline`.
        """

    class CreateParamsMandateAcceptanceOnline(TypedDict):
        date: NotRequired[int]
        """
        The Unix timestamp (in seconds) when the mandate was accepted or refused by the customer.
        """
        ip: NotRequired[str]
        """
        The IP address from which the mandate was accepted or refused by the customer.
        """
        user_agent: NotRequired[str]
        """
        The user agent of the browser from which the mandate was accepted or refused by the customer.
        """

    class CreateParamsOwner(TypedDict):
        address: NotRequired["Source.CreateParamsOwnerAddress"]
        """
        Owner's address.
        """
        email: NotRequired[str]
        """
        Owner's email address.
        """
        name: NotRequired[str]
        """
        Owner's full name.
        """
        phone: NotRequired[str]
        """
        Owner's phone number.
        """

    class CreateParamsOwnerAddress(TypedDict):
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

    class CreateParamsReceiver(TypedDict):
        refund_attributes_method: NotRequired[
            Literal["email", "manual", "none"]
        ]
        """
        The method Stripe should use to request information needed to process a refund or mispayment. Either `email` (an email is sent directly to the customer) or `manual` (a `source.refund_attributes_required` event is sent to your webhooks endpoint). Refer to each payment method's documentation to learn which refund attributes may be required.
        """

    class CreateParamsRedirect(TypedDict):
        return_url: str
        """
        The URL you provide to redirect the customer back to you after they authenticated their payment. It can use your application URI scheme in the context of a mobile application.
        """

    class CreateParamsSourceOrder(TypedDict):
        items: NotRequired[List["Source.CreateParamsSourceOrderItem"]]
        """
        List of items constituting the order.
        """
        shipping: NotRequired["Source.CreateParamsSourceOrderShipping"]
        """
        Shipping address for the order. Required if any of the SKUs are for products that have `shippable` set to true.
        """

    class CreateParamsSourceOrderItem(TypedDict):
        amount: NotRequired[int]
        currency: NotRequired[str]
        description: NotRequired[str]
        parent: NotRequired[str]
        """
        The ID of the SKU being ordered.
        """
        quantity: NotRequired[int]
        """
        The quantity of this order item. When type is `sku`, this is the number of instances of the SKU to be ordered.
        """
        type: NotRequired[Literal["discount", "shipping", "sku", "tax"]]

    class CreateParamsSourceOrderShipping(TypedDict):
        address: "Source.CreateParamsSourceOrderShippingAddress"
        """
        Shipping address.
        """
        carrier: NotRequired[str]
        """
        The delivery service that shipped a physical product, such as Fedex, UPS, USPS, etc.
        """
        name: NotRequired[str]
        """
        Recipient name.
        """
        phone: NotRequired[str]
        """
        Recipient phone (including extension).
        """
        tracking_number: NotRequired[str]
        """
        The tracking number for a physical product, obtained from the delivery service. If multiple tracking numbers were generated for this purchase, please separate them with commas.
        """

    class CreateParamsSourceOrderShippingAddress(TypedDict):
        city: NotRequired[str]
        """
        City, district, suburb, town, or village.
        """
        country: NotRequired[str]
        """
        Two-letter country code ([ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)).
        """
        line1: str
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

    class ListSourceTransactionsParams(RequestOptions):
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

    class ModifyParams(RequestOptions):
        amount: NotRequired[int]
        """
        Amount associated with the source.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        mandate: NotRequired["Source.ModifyParamsMandate"]
        """
        Information about a mandate possibility attached to a source object (generally for bank debits) as well as its acceptance status.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        owner: NotRequired["Source.ModifyParamsOwner"]
        """
        Information about the owner of the payment instrument that may be used or required by particular source types.
        """
        source_order: NotRequired["Source.ModifyParamsSourceOrder"]
        """
        Information about the items and shipping associated with the source. Required for transactional credit (for example Klarna) sources before you can charge it.
        """

    class ModifyParamsMandate(TypedDict):
        acceptance: NotRequired["Source.ModifyParamsMandateAcceptance"]
        """
        The parameters required to notify Stripe of a mandate acceptance or refusal by the customer.
        """
        amount: NotRequired["Literal['']|int"]
        """
        The amount specified by the mandate. (Leave null for a mandate covering all amounts)
        """
        currency: NotRequired[str]
        """
        The currency specified by the mandate. (Must match `currency` of the source)
        """
        interval: NotRequired[Literal["one_time", "scheduled", "variable"]]
        """
        The interval of debits permitted by the mandate. Either `one_time` (just permitting a single debit), `scheduled` (with debits on an agreed schedule or for clearly-defined events), or `variable`(for debits with any frequency)
        """
        notification_method: NotRequired[
            Literal[
                "deprecated_none", "email", "manual", "none", "stripe_email"
            ]
        ]
        """
        The method Stripe should use to notify the customer of upcoming debit instructions and/or mandate confirmation as required by the underlying debit network. Either `email` (an email is sent directly to the customer), `manual` (a `source.mandate_notification` event is sent to your webhooks endpoint and you should handle the notification) or `none` (the underlying debit network does not require any notification).
        """

    class ModifyParamsMandateAcceptance(TypedDict):
        date: NotRequired[int]
        """
        The Unix timestamp (in seconds) when the mandate was accepted or refused by the customer.
        """
        ip: NotRequired[str]
        """
        The IP address from which the mandate was accepted or refused by the customer.
        """
        offline: NotRequired["Source.ModifyParamsMandateAcceptanceOffline"]
        """
        The parameters required to store a mandate accepted offline. Should only be set if `mandate[type]` is `offline`
        """
        online: NotRequired["Source.ModifyParamsMandateAcceptanceOnline"]
        """
        The parameters required to store a mandate accepted online. Should only be set if `mandate[type]` is `online`
        """
        status: Literal["accepted", "pending", "refused", "revoked"]
        """
        The status of the mandate acceptance. Either `accepted` (the mandate was accepted) or `refused` (the mandate was refused).
        """
        type: NotRequired[Literal["offline", "online"]]
        """
        The type of acceptance information included with the mandate. Either `online` or `offline`
        """
        user_agent: NotRequired[str]
        """
        The user agent of the browser from which the mandate was accepted or refused by the customer.
        """

    class ModifyParamsMandateAcceptanceOffline(TypedDict):
        contact_email: str
        """
        An email to contact you with if a copy of the mandate is requested, required if `type` is `offline`.
        """

    class ModifyParamsMandateAcceptanceOnline(TypedDict):
        date: NotRequired[int]
        """
        The Unix timestamp (in seconds) when the mandate was accepted or refused by the customer.
        """
        ip: NotRequired[str]
        """
        The IP address from which the mandate was accepted or refused by the customer.
        """
        user_agent: NotRequired[str]
        """
        The user agent of the browser from which the mandate was accepted or refused by the customer.
        """

    class ModifyParamsOwner(TypedDict):
        address: NotRequired["Source.ModifyParamsOwnerAddress"]
        """
        Owner's address.
        """
        email: NotRequired[str]
        """
        Owner's email address.
        """
        name: NotRequired[str]
        """
        Owner's full name.
        """
        phone: NotRequired[str]
        """
        Owner's phone number.
        """

    class ModifyParamsOwnerAddress(TypedDict):
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

    class ModifyParamsSourceOrder(TypedDict):
        items: NotRequired[List["Source.ModifyParamsSourceOrderItem"]]
        """
        List of items constituting the order.
        """
        shipping: NotRequired["Source.ModifyParamsSourceOrderShipping"]
        """
        Shipping address for the order. Required if any of the SKUs are for products that have `shippable` set to true.
        """

    class ModifyParamsSourceOrderItem(TypedDict):
        amount: NotRequired[int]
        currency: NotRequired[str]
        description: NotRequired[str]
        parent: NotRequired[str]
        """
        The ID of the SKU being ordered.
        """
        quantity: NotRequired[int]
        """
        The quantity of this order item. When type is `sku`, this is the number of instances of the SKU to be ordered.
        """
        type: NotRequired[Literal["discount", "shipping", "sku", "tax"]]

    class ModifyParamsSourceOrderShipping(TypedDict):
        address: "Source.ModifyParamsSourceOrderShippingAddress"
        """
        Shipping address.
        """
        carrier: NotRequired[str]
        """
        The delivery service that shipped a physical product, such as Fedex, UPS, USPS, etc.
        """
        name: NotRequired[str]
        """
        Recipient name.
        """
        phone: NotRequired[str]
        """
        Recipient phone (including extension).
        """
        tracking_number: NotRequired[str]
        """
        The tracking number for a physical product, obtained from the delivery service. If multiple tracking numbers were generated for this purchase, please separate them with commas.
        """

    class ModifyParamsSourceOrderShippingAddress(TypedDict):
        city: NotRequired[str]
        """
        City, district, suburb, town, or village.
        """
        country: NotRequired[str]
        """
        Two-letter country code ([ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)).
        """
        line1: str
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

    class RetrieveParams(RequestOptions):
        client_secret: NotRequired[str]
        """
        The client secret of the source. Required if a publishable key is used to retrieve the source.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class VerifyParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        values: List[str]
        """
        The values needed to verify the source.
        """

    ach_credit_transfer: Optional[AchCreditTransfer]
    ach_debit: Optional[AchDebit]
    acss_debit: Optional[AcssDebit]
    alipay: Optional[Alipay]
    amount: Optional[int]
    """
    A positive integer in the smallest currency unit (that is, 100 cents for $1.00, or 1 for ¥1, Japanese Yen being a zero-decimal currency) representing the total amount associated with the source. This is the amount for which the source will be chargeable once ready. Required for `single_use` sources.
    """
    au_becs_debit: Optional[AuBecsDebit]
    bancontact: Optional[Bancontact]
    card: Optional[Card]
    card_present: Optional[CardPresent]
    client_secret: str
    """
    The client secret of the source. Used for client-side retrieval using a publishable key.
    """
    code_verification: Optional[CodeVerification]
    created: int
    """
    Time at which the object was created. Measured in seconds since the Unix epoch.
    """
    currency: Optional[str]
    """
    Three-letter [ISO code for the currency](https://stripe.com/docs/currencies) associated with the source. This is the currency for which the source will be chargeable once ready. Required for `single_use` sources.
    """
    customer: Optional[str]
    """
    The ID of the customer to which this source is attached. This will not be present when the source has not been attached to a customer.
    """
    eps: Optional[Eps]
    flow: str
    """
    The authentication `flow` of the source. `flow` is one of `redirect`, `receiver`, `code_verification`, `none`.
    """
    giropay: Optional[Giropay]
    id: str
    """
    Unique identifier for the object.
    """
    ideal: Optional[Ideal]
    klarna: Optional[Klarna]
    livemode: bool
    """
    Has the value `true` if the object exists in live mode or the value `false` if the object exists in test mode.
    """
    metadata: Optional[Dict[str, str]]
    """
    Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format.
    """
    multibanco: Optional[Multibanco]
    object: Literal["source"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """
    owner: Optional[Owner]
    """
    Information about the owner of the payment instrument that may be used or required by particular source types.
    """
    p24: Optional[P24]
    receiver: Optional[Receiver]
    redirect: Optional[Redirect]
    sepa_credit_transfer: Optional[SepaCreditTransfer]
    sepa_debit: Optional[SepaDebit]
    sofort: Optional[Sofort]
    source_order: Optional[SourceOrder]
    statement_descriptor: Optional[str]
    """
    Extra information about a source. This will appear on your customer's statement every time you charge the source.
    """
    status: str
    """
    The status of the source, one of `canceled`, `chargeable`, `consumed`, `failed`, or `pending`. Only `chargeable` sources can be used to create a charge.
    """
    three_d_secure: Optional[ThreeDSecure]
    type: Literal[
        "ach_credit_transfer",
        "ach_debit",
        "acss_debit",
        "alipay",
        "au_becs_debit",
        "bancontact",
        "card",
        "card_present",
        "eps",
        "giropay",
        "ideal",
        "klarna",
        "multibanco",
        "p24",
        "sepa_credit_transfer",
        "sepa_debit",
        "sofort",
        "three_d_secure",
        "wechat",
    ]
    """
    The `type` of the source. The `type` is a payment method, one of `ach_credit_transfer`, `ach_debit`, `alipay`, `bancontact`, `card`, `card_present`, `eps`, `giropay`, `ideal`, `multibanco`, `klarna`, `p24`, `sepa_debit`, `sofort`, `three_d_secure`, or `wechat`. An additional hash is included on the source with a name matching this value. It contains additional information specific to the [payment method](https://stripe.com/docs/sources) used.
    """
    usage: Optional[str]
    """
    Either `reusable` or `single_use`. Whether this source should be reusable or not. Some source types may or may not be reusable by construction, while others may leave the option at creation. If an incompatible value is passed, an error will be returned.
    """
    wechat: Optional[Wechat]

    @classmethod
    def create(cls, **params: Unpack["Source.CreateParams"]) -> "Source":
        """
        Creates a new source object.
        """
        return cast(
            "Source",
            cls._static_request(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    async def create_async(
        cls, **params: Unpack["Source.CreateParams"]
    ) -> "Source":
        """
        Creates a new source object.
        """
        return cast(
            "Source",
            await cls._static_request_async(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    def _cls_list_source_transactions(
        cls,
        source: str,
        **params: Unpack["Source.ListSourceTransactionsParams"],
    ) -> ListObject["SourceTransaction"]:
        """
        List source transactions for a given source.
        """
        return cast(
            ListObject["SourceTransaction"],
            cls._static_request(
                "get",
                "/v1/sources/{source}/source_transactions".format(
                    source=sanitize_id(source)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    def list_source_transactions(
        source: str, **params: Unpack["Source.ListSourceTransactionsParams"]
    ) -> ListObject["SourceTransaction"]:
        """
        List source transactions for a given source.
        """
        ...

    @overload
    def list_source_transactions(
        self, **params: Unpack["Source.ListSourceTransactionsParams"]
    ) -> ListObject["SourceTransaction"]:
        """
        List source transactions for a given source.
        """
        ...

    @class_method_variant("_cls_list_source_transactions")
    def list_source_transactions(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Source.ListSourceTransactionsParams"]
    ) -> ListObject["SourceTransaction"]:
        """
        List source transactions for a given source.
        """
        return cast(
            ListObject["SourceTransaction"],
            self._request(
                "get",
                "/v1/sources/{source}/source_transactions".format(
                    source=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    async def _cls_list_source_transactions_async(
        cls,
        source: str,
        **params: Unpack["Source.ListSourceTransactionsParams"],
    ) -> ListObject["SourceTransaction"]:
        """
        List source transactions for a given source.
        """
        return cast(
            ListObject["SourceTransaction"],
            await cls._static_request_async(
                "get",
                "/v1/sources/{source}/source_transactions".format(
                    source=sanitize_id(source)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    async def list_source_transactions_async(
        source: str, **params: Unpack["Source.ListSourceTransactionsParams"]
    ) -> ListObject["SourceTransaction"]:
        """
        List source transactions for a given source.
        """
        ...

    @overload
    async def list_source_transactions_async(
        self, **params: Unpack["Source.ListSourceTransactionsParams"]
    ) -> ListObject["SourceTransaction"]:
        """
        List source transactions for a given source.
        """
        ...

    @class_method_variant("_cls_list_source_transactions_async")
    async def list_source_transactions_async(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Source.ListSourceTransactionsParams"]
    ) -> ListObject["SourceTransaction"]:
        """
        List source transactions for a given source.
        """
        return cast(
            ListObject["SourceTransaction"],
            await self._request_async(
                "get",
                "/v1/sources/{source}/source_transactions".format(
                    source=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    def modify(
        cls, id: str, **params: Unpack["Source.ModifyParams"]
    ) -> "Source":
        """
        Updates the specified source by setting the values of the parameters passed. Any parameters not provided will be left unchanged.

        This request accepts the metadata and owner as arguments. It is also possible to update type specific information for selected payment methods. Please refer to our [payment method guides](https://stripe.com/docs/sources) for more detail.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(id))
        return cast(
            "Source",
            cls._static_request(
                "post",
                url,
                params=params,
            ),
        )

    @classmethod
    async def modify_async(
        cls, id: str, **params: Unpack["Source.ModifyParams"]
    ) -> "Source":
        """
        Updates the specified source by setting the values of the parameters passed. Any parameters not provided will be left unchanged.

        This request accepts the metadata and owner as arguments. It is also possible to update type specific information for selected payment methods. Please refer to our [payment method guides](https://stripe.com/docs/sources) for more detail.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(id))
        return cast(
            "Source",
            await cls._static_request_async(
                "post",
                url,
                params=params,
            ),
        )

    @classmethod
    def retrieve(
        cls, id: str, **params: Unpack["Source.RetrieveParams"]
    ) -> "Source":
        """
        Retrieves an existing source object. Supply the unique source ID from a source creation request and Stripe will return the corresponding up-to-date source object information.
        """
        instance = cls(id, **params)
        instance.refresh()
        return instance

    @classmethod
    async def retrieve_async(
        cls, id: str, **params: Unpack["Source.RetrieveParams"]
    ) -> "Source":
        """
        Retrieves an existing source object. Supply the unique source ID from a source creation request and Stripe will return the corresponding up-to-date source object information.
        """
        instance = cls(id, **params)
        await instance.refresh_async()
        return instance

    @classmethod
    def _cls_verify(
        cls, source: str, **params: Unpack["Source.VerifyParams"]
    ) -> "Source":
        """
        Verify a given source.
        """
        return cast(
            "Source",
            cls._static_request(
                "post",
                "/v1/sources/{source}/verify".format(
                    source=sanitize_id(source)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    def verify(
        source: str, **params: Unpack["Source.VerifyParams"]
    ) -> "Source":
        """
        Verify a given source.
        """
        ...

    @overload
    def verify(self, **params: Unpack["Source.VerifyParams"]) -> "Source":
        """
        Verify a given source.
        """
        ...

    @class_method_variant("_cls_verify")
    def verify(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Source.VerifyParams"]
    ) -> "Source":
        """
        Verify a given source.
        """
        return cast(
            "Source",
            self._request(
                "post",
                "/v1/sources/{source}/verify".format(
                    source=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    async def _cls_verify_async(
        cls, source: str, **params: Unpack["Source.VerifyParams"]
    ) -> "Source":
        """
        Verify a given source.
        """
        return cast(
            "Source",
            await cls._static_request_async(
                "post",
                "/v1/sources/{source}/verify".format(
                    source=sanitize_id(source)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    async def verify_async(
        source: str, **params: Unpack["Source.VerifyParams"]
    ) -> "Source":
        """
        Verify a given source.
        """
        ...

    @overload
    async def verify_async(
        self, **params: Unpack["Source.VerifyParams"]
    ) -> "Source":
        """
        Verify a given source.
        """
        ...

    @class_method_variant("_cls_verify_async")
    async def verify_async(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Source.VerifyParams"]
    ) -> "Source":
        """
        Verify a given source.
        """
        return cast(
            "Source",
            await self._request_async(
                "post",
                "/v1/sources/{source}/verify".format(
                    source=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    def detach(self, **params) -> "Source":
        token = self.id

        if hasattr(self, "customer") and self.customer:
            extn = sanitize_id(token)
            customer = self.customer
            base = Customer.class_url()
            owner_extn = sanitize_id(customer)
            url = "%s/%s/sources/%s" % (base, owner_extn, extn)

            self._request_and_refresh("delete", url, params)
            return cast("Source", self)

        else:
            raise InvalidRequestError(
                "Source %s does not appear to be currently attached "
                "to a customer object." % token,
                "id",
            )

    _inner_class_types = {
        "ach_credit_transfer": AchCreditTransfer,
        "ach_debit": AchDebit,
        "acss_debit": AcssDebit,
        "alipay": Alipay,
        "au_becs_debit": AuBecsDebit,
        "bancontact": Bancontact,
        "card": Card,
        "card_present": CardPresent,
        "code_verification": CodeVerification,
        "eps": Eps,
        "giropay": Giropay,
        "ideal": Ideal,
        "klarna": Klarna,
        "multibanco": Multibanco,
        "owner": Owner,
        "p24": P24,
        "receiver": Receiver,
        "redirect": Redirect,
        "sepa_credit_transfer": SepaCreditTransfer,
        "sepa_debit": SepaDebit,
        "sofort": Sofort,
        "source_order": SourceOrder,
        "three_d_secure": ThreeDSecure,
        "wechat": Wechat,
    }

# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._expandable_field import ExpandableField
from stripe._list_object import ListObject
from stripe._listable_api_resource import ListableAPIResource
from stripe._request_options import RequestOptions
from stripe._stripe_object import StripeObject
from stripe._test_helpers import APIResourceTestHelpers
from stripe._updateable_api_resource import UpdateableAPIResource
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
    from stripe._balance_transaction import BalanceTransaction
    from stripe.issuing._card import Card
    from stripe.issuing._cardholder import Cardholder
    from stripe.issuing._token import Token
    from stripe.issuing._transaction import Transaction


class Authorization(
    ListableAPIResource["Authorization"],
    UpdateableAPIResource["Authorization"],
):
    """
    When an [issued card](https://stripe.com/docs/issuing) is used to make a purchase, an Issuing `Authorization`
    object is created. [Authorizations](https://stripe.com/docs/issuing/purchases/authorizations) must be approved for the
    purchase to be completed successfully.

    Related guide: [Issued card authorizations](https://stripe.com/docs/issuing/purchases/authorizations)
    """

    OBJECT_NAME: ClassVar[Literal["issuing.authorization"]] = (
        "issuing.authorization"
    )

    class AmountDetails(StripeObject):
        atm_fee: Optional[int]
        """
        The fee charged by the ATM for the cash withdrawal.
        """
        cashback_amount: Optional[int]
        """
        The amount of cash requested by the cardholder.
        """

    class Fleet(StripeObject):
        class CardholderPromptData(StripeObject):
            alphanumeric_id: Optional[str]
            """
            [Deprecated] An alphanumeric ID, though typical point of sales only support numeric entry. The card program can be configured to prompt for a vehicle ID, driver ID, or generic ID.
            """
            driver_id: Optional[str]
            """
            Driver ID.
            """
            odometer: Optional[int]
            """
            Odometer reading.
            """
            unspecified_id: Optional[str]
            """
            An alphanumeric ID. This field is used when a vehicle ID, driver ID, or generic ID is entered by the cardholder, but the merchant or card network did not specify the prompt type.
            """
            user_id: Optional[str]
            """
            User ID.
            """
            vehicle_number: Optional[str]
            """
            Vehicle number.
            """

        class ReportedBreakdown(StripeObject):
            class Fuel(StripeObject):
                gross_amount_decimal: Optional[str]
                """
                Gross fuel amount that should equal Fuel Quantity multiplied by Fuel Unit Cost, inclusive of taxes.
                """

            class NonFuel(StripeObject):
                gross_amount_decimal: Optional[str]
                """
                Gross non-fuel amount that should equal the sum of the line items, inclusive of taxes.
                """

            class Tax(StripeObject):
                local_amount_decimal: Optional[str]
                """
                Amount of state or provincial Sales Tax included in the transaction amount. `null` if not reported by merchant or not subject to tax.
                """
                national_amount_decimal: Optional[str]
                """
                Amount of national Sales Tax or VAT included in the transaction amount. `null` if not reported by merchant or not subject to tax.
                """

            fuel: Optional[Fuel]
            """
            Breakdown of fuel portion of the purchase.
            """
            non_fuel: Optional[NonFuel]
            """
            Breakdown of non-fuel portion of the purchase.
            """
            tax: Optional[Tax]
            """
            Information about tax included in this transaction.
            """
            _inner_class_types = {
                "fuel": Fuel,
                "non_fuel": NonFuel,
                "tax": Tax,
            }

        cardholder_prompt_data: Optional[CardholderPromptData]
        """
        Answers to prompts presented to the cardholder at the point of sale. Prompted fields vary depending on the configuration of your physical fleet cards. Typical points of sale support only numeric entry.
        """
        purchase_type: Optional[
            Literal[
                "fuel_and_non_fuel_purchase",
                "fuel_purchase",
                "non_fuel_purchase",
            ]
        ]
        """
        The type of purchase.
        """
        reported_breakdown: Optional[ReportedBreakdown]
        """
        More information about the total amount. Typically this information is received from the merchant after the authorization has been approved and the fuel dispensed. This information is not guaranteed to be accurate as some merchants may provide unreliable data.
        """
        service_type: Optional[
            Literal["full_service", "non_fuel_transaction", "self_service"]
        ]
        """
        The type of fuel service.
        """
        _inner_class_types = {
            "cardholder_prompt_data": CardholderPromptData,
            "reported_breakdown": ReportedBreakdown,
        }

    class Fuel(StripeObject):
        industry_product_code: Optional[str]
        """
        [Conexxus Payment System Product Code](https://www.conexxus.org/conexxus-payment-system-product-codes) identifying the primary fuel product purchased.
        """
        quantity_decimal: Optional[str]
        """
        The quantity of `unit`s of fuel that was dispensed, represented as a decimal string with at most 12 decimal places.
        """
        type: Optional[
            Literal[
                "diesel",
                "other",
                "unleaded_plus",
                "unleaded_regular",
                "unleaded_super",
            ]
        ]
        """
        The type of fuel that was purchased.
        """
        unit: Optional[
            Literal[
                "charging_minute",
                "imperial_gallon",
                "kilogram",
                "kilowatt_hour",
                "liter",
                "other",
                "pound",
                "us_gallon",
            ]
        ]
        """
        The units for `quantity_decimal`.
        """
        unit_cost_decimal: Optional[str]
        """
        The cost in cents per each unit of fuel, represented as a decimal string with at most 12 decimal places.
        """

    class MerchantData(StripeObject):
        category: str
        """
        A categorization of the seller's type of business. See our [merchant categories guide](https://stripe.com/docs/issuing/merchant-categories) for a list of possible values.
        """
        category_code: str
        """
        The merchant category code for the seller's business
        """
        city: Optional[str]
        """
        City where the seller is located
        """
        country: Optional[str]
        """
        Country where the seller is located
        """
        name: Optional[str]
        """
        Name of the seller
        """
        network_id: str
        """
        Identifier assigned to the seller by the card network. Different card networks may assign different network_id fields to the same merchant.
        """
        postal_code: Optional[str]
        """
        Postal code where the seller is located
        """
        state: Optional[str]
        """
        State where the seller is located
        """
        terminal_id: Optional[str]
        """
        An ID assigned by the seller to the location of the sale.
        """
        url: Optional[str]
        """
        URL provided by the merchant on a 3DS request
        """

    class NetworkData(StripeObject):
        acquiring_institution_id: Optional[str]
        """
        Identifier assigned to the acquirer by the card network. Sometimes this value is not provided by the network; in this case, the value will be `null`.
        """
        system_trace_audit_number: Optional[str]
        """
        The System Trace Audit Number (STAN) is a 6-digit identifier assigned by the acquirer. Prefer `network_data.transaction_id` if present, unless you have special requirements.
        """
        transaction_id: Optional[str]
        """
        Unique identifier for the authorization assigned by the card network used to match subsequent messages, disputes, and transactions.
        """

    class PendingRequest(StripeObject):
        class AmountDetails(StripeObject):
            atm_fee: Optional[int]
            """
            The fee charged by the ATM for the cash withdrawal.
            """
            cashback_amount: Optional[int]
            """
            The amount of cash requested by the cardholder.
            """

        amount: int
        """
        The additional amount Stripe will hold if the authorization is approved, in the card's [currency](https://stripe.com/docs/api#issuing_authorization_object-pending-request-currency) and in the [smallest currency unit](https://stripe.com/docs/currencies#zero-decimal).
        """
        amount_details: Optional[AmountDetails]
        """
        Detailed breakdown of amount components. These amounts are denominated in `currency` and in the [smallest currency unit](https://stripe.com/docs/currencies#zero-decimal).
        """
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        is_amount_controllable: bool
        """
        If set `true`, you may provide [amount](https://stripe.com/docs/api/issuing/authorizations/approve#approve_issuing_authorization-amount) to control how much to hold for the authorization.
        """
        merchant_amount: int
        """
        The amount the merchant is requesting to be authorized in the `merchant_currency`. The amount is in the [smallest currency unit](https://stripe.com/docs/currencies#zero-decimal).
        """
        merchant_currency: str
        """
        The local currency the merchant is requesting to authorize.
        """
        network_risk_score: Optional[int]
        """
        The card network's estimate of the likelihood that an authorization is fraudulent. Takes on values between 1 and 99.
        """
        _inner_class_types = {"amount_details": AmountDetails}

    class RequestHistory(StripeObject):
        class AmountDetails(StripeObject):
            atm_fee: Optional[int]
            """
            The fee charged by the ATM for the cash withdrawal.
            """
            cashback_amount: Optional[int]
            """
            The amount of cash requested by the cardholder.
            """

        amount: int
        """
        The `pending_request.amount` at the time of the request, presented in your card's currency and in the [smallest currency unit](https://stripe.com/docs/currencies#zero-decimal). Stripe held this amount from your account to fund the authorization if the request was approved.
        """
        amount_details: Optional[AmountDetails]
        """
        Detailed breakdown of amount components. These amounts are denominated in `currency` and in the [smallest currency unit](https://stripe.com/docs/currencies#zero-decimal).
        """
        approved: bool
        """
        Whether this request was approved.
        """
        authorization_code: Optional[str]
        """
        A code created by Stripe which is shared with the merchant to validate the authorization. This field will be populated if the authorization message was approved. The code typically starts with the letter "S", followed by a six-digit number. For example, "S498162". Please note that the code is not guaranteed to be unique across authorizations.
        """
        created: int
        """
        Time at which the object was created. Measured in seconds since the Unix epoch.
        """
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        merchant_amount: int
        """
        The `pending_request.merchant_amount` at the time of the request, presented in the `merchant_currency` and in the [smallest currency unit](https://stripe.com/docs/currencies#zero-decimal).
        """
        merchant_currency: str
        """
        The currency that was collected by the merchant and presented to the cardholder for the authorization. Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        network_risk_score: Optional[int]
        """
        The card network's estimate of the likelihood that an authorization is fraudulent. Takes on values between 1 and 99.
        """
        reason: Literal[
            "account_disabled",
            "card_active",
            "card_canceled",
            "card_expired",
            "card_inactive",
            "cardholder_blocked",
            "cardholder_inactive",
            "cardholder_verification_required",
            "insecure_authorization_method",
            "insufficient_funds",
            "not_allowed",
            "pin_blocked",
            "spending_controls",
            "suspected_fraud",
            "verification_failed",
            "webhook_approved",
            "webhook_declined",
            "webhook_error",
            "webhook_timeout",
        ]
        """
        When an authorization is approved or declined by you or by Stripe, this field provides additional detail on the reason for the outcome.
        """
        reason_message: Optional[str]
        """
        If the `request_history.reason` is `webhook_error` because the direct webhook response is invalid (for example, parsing errors or missing parameters), we surface a more detailed error message via this field.
        """
        requested_at: Optional[int]
        """
        Time when the card network received an authorization request from the acquirer in UTC. Referred to by networks as transmission time.
        """
        _inner_class_types = {"amount_details": AmountDetails}

    class Treasury(StripeObject):
        received_credits: List[str]
        """
        The array of [ReceivedCredits](https://stripe.com/docs/api/treasury/received_credits) associated with this authorization
        """
        received_debits: List[str]
        """
        The array of [ReceivedDebits](https://stripe.com/docs/api/treasury/received_debits) associated with this authorization
        """
        transaction: Optional[str]
        """
        The Treasury [Transaction](https://stripe.com/docs/api/treasury/transactions) associated with this authorization
        """

    class VerificationData(StripeObject):
        class AuthenticationExemption(StripeObject):
            claimed_by: Literal["acquirer", "issuer"]
            """
            The entity that requested the exemption, either the acquiring merchant or the Issuing user.
            """
            type: Literal[
                "low_value_transaction", "transaction_risk_analysis", "unknown"
            ]
            """
            The specific exemption claimed for this authorization.
            """

        class ThreeDSecure(StripeObject):
            result: Literal[
                "attempt_acknowledged", "authenticated", "failed", "required"
            ]
            """
            The outcome of the 3D Secure authentication request.
            """

        address_line1_check: Literal["match", "mismatch", "not_provided"]
        """
        Whether the cardholder provided an address first line and if it matched the cardholder's `billing.address.line1`.
        """
        address_postal_code_check: Literal["match", "mismatch", "not_provided"]
        """
        Whether the cardholder provided a postal code and if it matched the cardholder's `billing.address.postal_code`.
        """
        authentication_exemption: Optional[AuthenticationExemption]
        """
        The exemption applied to this authorization.
        """
        cvc_check: Literal["match", "mismatch", "not_provided"]
        """
        Whether the cardholder provided a CVC and if it matched Stripe's record.
        """
        expiry_check: Literal["match", "mismatch", "not_provided"]
        """
        Whether the cardholder provided an expiry date and if it matched Stripe's record.
        """
        postal_code: Optional[str]
        """
        The postal code submitted as part of the authorization used for postal code verification.
        """
        three_d_secure: Optional[ThreeDSecure]
        """
        3D Secure details.
        """
        _inner_class_types = {
            "authentication_exemption": AuthenticationExemption,
            "three_d_secure": ThreeDSecure,
        }

    class ApproveParams(RequestOptions):
        amount: NotRequired[int]
        """
        If the authorization's `pending_request.is_amount_controllable` property is `true`, you may provide this value to control how much to hold for the authorization. Must be positive (use [`decline`](https://stripe.com/docs/api/issuing/authorizations/decline) to decline an authorization request).
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """

    class CaptureParams(RequestOptions):
        capture_amount: NotRequired[int]
        """
        The amount to capture from the authorization. If not provided, the full amount of the authorization will be captured. This amount is in the authorization currency and in the [smallest currency unit](https://stripe.com/docs/currencies#zero-decimal).
        """
        close_authorization: NotRequired[bool]
        """
        Whether to close the authorization after capture. Defaults to true. Set to false to enable multi-capture flows.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        purchase_details: NotRequired[
            "Authorization.CaptureParamsPurchaseDetails"
        ]
        """
        Additional purchase information that is optionally provided by the merchant.
        """

    class CaptureParamsPurchaseDetails(TypedDict):
        fleet: NotRequired["Authorization.CaptureParamsPurchaseDetailsFleet"]
        """
        Fleet-specific information for transactions using Fleet cards.
        """
        flight: NotRequired["Authorization.CaptureParamsPurchaseDetailsFlight"]
        """
        Information about the flight that was purchased with this transaction.
        """
        fuel: NotRequired["Authorization.CaptureParamsPurchaseDetailsFuel"]
        """
        Information about fuel that was purchased with this transaction.
        """
        lodging: NotRequired[
            "Authorization.CaptureParamsPurchaseDetailsLodging"
        ]
        """
        Information about lodging that was purchased with this transaction.
        """
        receipt: NotRequired[
            List["Authorization.CaptureParamsPurchaseDetailsReceipt"]
        ]
        """
        The line items in the purchase.
        """
        reference: NotRequired[str]
        """
        A merchant-specific order number.
        """

    class CaptureParamsPurchaseDetailsFleet(TypedDict):
        cardholder_prompt_data: NotRequired[
            "Authorization.CaptureParamsPurchaseDetailsFleetCardholderPromptData"
        ]
        """
        Answers to prompts presented to the cardholder at the point of sale. Prompted fields vary depending on the configuration of your physical fleet cards. Typical points of sale support only numeric entry.
        """
        purchase_type: NotRequired[
            Literal[
                "fuel_and_non_fuel_purchase",
                "fuel_purchase",
                "non_fuel_purchase",
            ]
        ]
        """
        The type of purchase. One of `fuel_purchase`, `non_fuel_purchase`, or `fuel_and_non_fuel_purchase`.
        """
        reported_breakdown: NotRequired[
            "Authorization.CaptureParamsPurchaseDetailsFleetReportedBreakdown"
        ]
        """
        More information about the total amount. This information is not guaranteed to be accurate as some merchants may provide unreliable data.
        """
        service_type: NotRequired[
            Literal["full_service", "non_fuel_transaction", "self_service"]
        ]
        """
        The type of fuel service. One of `non_fuel_transaction`, `full_service`, or `self_service`.
        """

    class CaptureParamsPurchaseDetailsFleetCardholderPromptData(TypedDict):
        driver_id: NotRequired[str]
        """
        Driver ID.
        """
        odometer: NotRequired[int]
        """
        Odometer reading.
        """
        unspecified_id: NotRequired[str]
        """
        An alphanumeric ID. This field is used when a vehicle ID, driver ID, or generic ID is entered by the cardholder, but the merchant or card network did not specify the prompt type.
        """
        user_id: NotRequired[str]
        """
        User ID.
        """
        vehicle_number: NotRequired[str]
        """
        Vehicle number.
        """

    class CaptureParamsPurchaseDetailsFleetReportedBreakdown(TypedDict):
        fuel: NotRequired[
            "Authorization.CaptureParamsPurchaseDetailsFleetReportedBreakdownFuel"
        ]
        """
        Breakdown of fuel portion of the purchase.
        """
        non_fuel: NotRequired[
            "Authorization.CaptureParamsPurchaseDetailsFleetReportedBreakdownNonFuel"
        ]
        """
        Breakdown of non-fuel portion of the purchase.
        """
        tax: NotRequired[
            "Authorization.CaptureParamsPurchaseDetailsFleetReportedBreakdownTax"
        ]
        """
        Information about tax included in this transaction.
        """

    class CaptureParamsPurchaseDetailsFleetReportedBreakdownFuel(TypedDict):
        gross_amount_decimal: NotRequired[str]
        """
        Gross fuel amount that should equal Fuel Volume multipled by Fuel Unit Cost, inclusive of taxes.
        """

    class CaptureParamsPurchaseDetailsFleetReportedBreakdownNonFuel(TypedDict):
        gross_amount_decimal: NotRequired[str]
        """
        Gross non-fuel amount that should equal the sum of the line items, inclusive of taxes.
        """

    class CaptureParamsPurchaseDetailsFleetReportedBreakdownTax(TypedDict):
        local_amount_decimal: NotRequired[str]
        """
        Amount of state or provincial Sales Tax included in the transaction amount. Null if not reported by merchant or not subject to tax.
        """
        national_amount_decimal: NotRequired[str]
        """
        Amount of national Sales Tax or VAT included in the transaction amount. Null if not reported by merchant or not subject to tax.
        """

    class CaptureParamsPurchaseDetailsFlight(TypedDict):
        departure_at: NotRequired[int]
        """
        The time that the flight departed.
        """
        passenger_name: NotRequired[str]
        """
        The name of the passenger.
        """
        refundable: NotRequired[bool]
        """
        Whether the ticket is refundable.
        """
        segments: NotRequired[
            List["Authorization.CaptureParamsPurchaseDetailsFlightSegment"]
        ]
        """
        The legs of the trip.
        """
        travel_agency: NotRequired[str]
        """
        The travel agency that issued the ticket.
        """

    class CaptureParamsPurchaseDetailsFlightSegment(TypedDict):
        arrival_airport_code: NotRequired[str]
        """
        The three-letter IATA airport code of the flight's destination.
        """
        carrier: NotRequired[str]
        """
        The airline carrier code.
        """
        departure_airport_code: NotRequired[str]
        """
        The three-letter IATA airport code that the flight departed from.
        """
        flight_number: NotRequired[str]
        """
        The flight number.
        """
        service_class: NotRequired[str]
        """
        The flight's service class.
        """
        stopover_allowed: NotRequired[bool]
        """
        Whether a stopover is allowed on this flight.
        """

    class CaptureParamsPurchaseDetailsFuel(TypedDict):
        industry_product_code: NotRequired[str]
        """
        [Conexxus Payment System Product Code](https://www.conexxus.org/conexxus-payment-system-product-codes) identifying the primary fuel product purchased.
        """
        quantity_decimal: NotRequired[str]
        """
        The quantity of `unit`s of fuel that was dispensed, represented as a decimal string with at most 12 decimal places.
        """
        type: NotRequired[
            Literal[
                "diesel",
                "other",
                "unleaded_plus",
                "unleaded_regular",
                "unleaded_super",
            ]
        ]
        """
        The type of fuel that was purchased. One of `diesel`, `unleaded_plus`, `unleaded_regular`, `unleaded_super`, or `other`.
        """
        unit: NotRequired[
            Literal[
                "charging_minute",
                "imperial_gallon",
                "kilogram",
                "kilowatt_hour",
                "liter",
                "other",
                "pound",
                "us_gallon",
            ]
        ]
        """
        The units for `quantity_decimal`. One of `charging_minute`, `imperial_gallon`, `kilogram`, `kilowatt_hour`, `liter`, `pound`, `us_gallon`, or `other`.
        """
        unit_cost_decimal: NotRequired[str]
        """
        The cost in cents per each unit of fuel, represented as a decimal string with at most 12 decimal places.
        """

    class CaptureParamsPurchaseDetailsLodging(TypedDict):
        check_in_at: NotRequired[int]
        """
        The time of checking into the lodging.
        """
        nights: NotRequired[int]
        """
        The number of nights stayed at the lodging.
        """

    class CaptureParamsPurchaseDetailsReceipt(TypedDict):
        description: NotRequired[str]
        quantity: NotRequired[str]
        total: NotRequired[int]
        unit_cost: NotRequired[int]

    class CreateParams(RequestOptions):
        amount: int
        """
        The total amount to attempt to authorize. This amount is in the provided currency, or defaults to the card's currency, and in the [smallest currency unit](https://stripe.com/docs/currencies#zero-decimal).
        """
        amount_details: NotRequired["Authorization.CreateParamsAmountDetails"]
        """
        Detailed breakdown of amount components. These amounts are denominated in `currency` and in the [smallest currency unit](https://stripe.com/docs/currencies#zero-decimal).
        """
        authorization_method: NotRequired[
            Literal["chip", "contactless", "keyed_in", "online", "swipe"]
        ]
        """
        How the card details were provided. Defaults to online.
        """
        card: str
        """
        Card associated with this authorization.
        """
        currency: NotRequired[str]
        """
        The currency of the authorization. If not provided, defaults to the currency of the card. Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        fleet: NotRequired["Authorization.CreateParamsFleet"]
        """
        Fleet-specific information for authorizations using Fleet cards.
        """
        fuel: NotRequired["Authorization.CreateParamsFuel"]
        """
        Information about fuel that was purchased with this transaction.
        """
        is_amount_controllable: NotRequired[bool]
        """
        If set `true`, you may provide [amount](https://stripe.com/docs/api/issuing/authorizations/approve#approve_issuing_authorization-amount) to control how much to hold for the authorization.
        """
        merchant_data: NotRequired["Authorization.CreateParamsMerchantData"]
        """
        Details about the seller (grocery store, e-commerce website, etc.) where the card authorization happened.
        """
        network_data: NotRequired["Authorization.CreateParamsNetworkData"]
        """
        Details about the authorization, such as identifiers, set by the card network.
        """
        verification_data: NotRequired[
            "Authorization.CreateParamsVerificationData"
        ]
        """
        Verifications that Stripe performed on information that the cardholder provided to the merchant.
        """
        wallet: NotRequired[Literal["apple_pay", "google_pay", "samsung_pay"]]
        """
        The digital wallet used for this transaction. One of `apple_pay`, `google_pay`, or `samsung_pay`. Will populate as `null` when no digital wallet was utilized.
        """

    class CreateParamsAmountDetails(TypedDict):
        atm_fee: NotRequired[int]
        """
        The ATM withdrawal fee.
        """
        cashback_amount: NotRequired[int]
        """
        The amount of cash requested by the cardholder.
        """

    class CreateParamsFleet(TypedDict):
        cardholder_prompt_data: NotRequired[
            "Authorization.CreateParamsFleetCardholderPromptData"
        ]
        """
        Answers to prompts presented to the cardholder at the point of sale. Prompted fields vary depending on the configuration of your physical fleet cards. Typical points of sale support only numeric entry.
        """
        purchase_type: NotRequired[
            Literal[
                "fuel_and_non_fuel_purchase",
                "fuel_purchase",
                "non_fuel_purchase",
            ]
        ]
        """
        The type of purchase. One of `fuel_purchase`, `non_fuel_purchase`, or `fuel_and_non_fuel_purchase`.
        """
        reported_breakdown: NotRequired[
            "Authorization.CreateParamsFleetReportedBreakdown"
        ]
        """
        More information about the total amount. This information is not guaranteed to be accurate as some merchants may provide unreliable data.
        """
        service_type: NotRequired[
            Literal["full_service", "non_fuel_transaction", "self_service"]
        ]
        """
        The type of fuel service. One of `non_fuel_transaction`, `full_service`, or `self_service`.
        """

    class CreateParamsFleetCardholderPromptData(TypedDict):
        driver_id: NotRequired[str]
        """
        Driver ID.
        """
        odometer: NotRequired[int]
        """
        Odometer reading.
        """
        unspecified_id: NotRequired[str]
        """
        An alphanumeric ID. This field is used when a vehicle ID, driver ID, or generic ID is entered by the cardholder, but the merchant or card network did not specify the prompt type.
        """
        user_id: NotRequired[str]
        """
        User ID.
        """
        vehicle_number: NotRequired[str]
        """
        Vehicle number.
        """

    class CreateParamsFleetReportedBreakdown(TypedDict):
        fuel: NotRequired[
            "Authorization.CreateParamsFleetReportedBreakdownFuel"
        ]
        """
        Breakdown of fuel portion of the purchase.
        """
        non_fuel: NotRequired[
            "Authorization.CreateParamsFleetReportedBreakdownNonFuel"
        ]
        """
        Breakdown of non-fuel portion of the purchase.
        """
        tax: NotRequired["Authorization.CreateParamsFleetReportedBreakdownTax"]
        """
        Information about tax included in this transaction.
        """

    class CreateParamsFleetReportedBreakdownFuel(TypedDict):
        gross_amount_decimal: NotRequired[str]
        """
        Gross fuel amount that should equal Fuel Volume multipled by Fuel Unit Cost, inclusive of taxes.
        """

    class CreateParamsFleetReportedBreakdownNonFuel(TypedDict):
        gross_amount_decimal: NotRequired[str]
        """
        Gross non-fuel amount that should equal the sum of the line items, inclusive of taxes.
        """

    class CreateParamsFleetReportedBreakdownTax(TypedDict):
        local_amount_decimal: NotRequired[str]
        """
        Amount of state or provincial Sales Tax included in the transaction amount. Null if not reported by merchant or not subject to tax.
        """
        national_amount_decimal: NotRequired[str]
        """
        Amount of national Sales Tax or VAT included in the transaction amount. Null if not reported by merchant or not subject to tax.
        """

    class CreateParamsFuel(TypedDict):
        industry_product_code: NotRequired[str]
        """
        [Conexxus Payment System Product Code](https://www.conexxus.org/conexxus-payment-system-product-codes) identifying the primary fuel product purchased.
        """
        quantity_decimal: NotRequired[str]
        """
        The quantity of `unit`s of fuel that was dispensed, represented as a decimal string with at most 12 decimal places.
        """
        type: NotRequired[
            Literal[
                "diesel",
                "other",
                "unleaded_plus",
                "unleaded_regular",
                "unleaded_super",
            ]
        ]
        """
        The type of fuel that was purchased. One of `diesel`, `unleaded_plus`, `unleaded_regular`, `unleaded_super`, or `other`.
        """
        unit: NotRequired[
            Literal[
                "charging_minute",
                "imperial_gallon",
                "kilogram",
                "kilowatt_hour",
                "liter",
                "other",
                "pound",
                "us_gallon",
            ]
        ]
        """
        The units for `quantity_decimal`. One of `charging_minute`, `imperial_gallon`, `kilogram`, `kilowatt_hour`, `liter`, `pound`, `us_gallon`, or `other`.
        """
        unit_cost_decimal: NotRequired[str]
        """
        The cost in cents per each unit of fuel, represented as a decimal string with at most 12 decimal places.
        """

    class CreateParamsMerchantData(TypedDict):
        category: NotRequired[
            Literal[
                "ac_refrigeration_repair",
                "accounting_bookkeeping_services",
                "advertising_services",
                "agricultural_cooperative",
                "airlines_air_carriers",
                "airports_flying_fields",
                "ambulance_services",
                "amusement_parks_carnivals",
                "antique_reproductions",
                "antique_shops",
                "aquariums",
                "architectural_surveying_services",
                "art_dealers_and_galleries",
                "artists_supply_and_craft_shops",
                "auto_and_home_supply_stores",
                "auto_body_repair_shops",
                "auto_paint_shops",
                "auto_service_shops",
                "automated_cash_disburse",
                "automated_fuel_dispensers",
                "automobile_associations",
                "automotive_parts_and_accessories_stores",
                "automotive_tire_stores",
                "bail_and_bond_payments",
                "bakeries",
                "bands_orchestras",
                "barber_and_beauty_shops",
                "betting_casino_gambling",
                "bicycle_shops",
                "billiard_pool_establishments",
                "boat_dealers",
                "boat_rentals_and_leases",
                "book_stores",
                "books_periodicals_and_newspapers",
                "bowling_alleys",
                "bus_lines",
                "business_secretarial_schools",
                "buying_shopping_services",
                "cable_satellite_and_other_pay_television_and_radio",
                "camera_and_photographic_supply_stores",
                "candy_nut_and_confectionery_stores",
                "car_and_truck_dealers_new_used",
                "car_and_truck_dealers_used_only",
                "car_rental_agencies",
                "car_washes",
                "carpentry_services",
                "carpet_upholstery_cleaning",
                "caterers",
                "charitable_and_social_service_organizations_fundraising",
                "chemicals_and_allied_products",
                "child_care_services",
                "childrens_and_infants_wear_stores",
                "chiropodists_podiatrists",
                "chiropractors",
                "cigar_stores_and_stands",
                "civic_social_fraternal_associations",
                "cleaning_and_maintenance",
                "clothing_rental",
                "colleges_universities",
                "commercial_equipment",
                "commercial_footwear",
                "commercial_photography_art_and_graphics",
                "commuter_transport_and_ferries",
                "computer_network_services",
                "computer_programming",
                "computer_repair",
                "computer_software_stores",
                "computers_peripherals_and_software",
                "concrete_work_services",
                "construction_materials",
                "consulting_public_relations",
                "correspondence_schools",
                "cosmetic_stores",
                "counseling_services",
                "country_clubs",
                "courier_services",
                "court_costs",
                "credit_reporting_agencies",
                "cruise_lines",
                "dairy_products_stores",
                "dance_hall_studios_schools",
                "dating_escort_services",
                "dentists_orthodontists",
                "department_stores",
                "detective_agencies",
                "digital_goods_applications",
                "digital_goods_games",
                "digital_goods_large_volume",
                "digital_goods_media",
                "direct_marketing_catalog_merchant",
                "direct_marketing_combination_catalog_and_retail_merchant",
                "direct_marketing_inbound_telemarketing",
                "direct_marketing_insurance_services",
                "direct_marketing_other",
                "direct_marketing_outbound_telemarketing",
                "direct_marketing_subscription",
                "direct_marketing_travel",
                "discount_stores",
                "doctors",
                "door_to_door_sales",
                "drapery_window_covering_and_upholstery_stores",
                "drinking_places",
                "drug_stores_and_pharmacies",
                "drugs_drug_proprietaries_and_druggist_sundries",
                "dry_cleaners",
                "durable_goods",
                "duty_free_stores",
                "eating_places_restaurants",
                "educational_services",
                "electric_razor_stores",
                "electric_vehicle_charging",
                "electrical_parts_and_equipment",
                "electrical_services",
                "electronics_repair_shops",
                "electronics_stores",
                "elementary_secondary_schools",
                "emergency_services_gcas_visa_use_only",
                "employment_temp_agencies",
                "equipment_rental",
                "exterminating_services",
                "family_clothing_stores",
                "fast_food_restaurants",
                "financial_institutions",
                "fines_government_administrative_entities",
                "fireplace_fireplace_screens_and_accessories_stores",
                "floor_covering_stores",
                "florists",
                "florists_supplies_nursery_stock_and_flowers",
                "freezer_and_locker_meat_provisioners",
                "fuel_dealers_non_automotive",
                "funeral_services_crematories",
                "furniture_home_furnishings_and_equipment_stores_except_appliances",
                "furniture_repair_refinishing",
                "furriers_and_fur_shops",
                "general_services",
                "gift_card_novelty_and_souvenir_shops",
                "glass_paint_and_wallpaper_stores",
                "glassware_crystal_stores",
                "golf_courses_public",
                "government_licensed_horse_dog_racing_us_region_only",
                "government_licensed_online_casions_online_gambling_us_region_only",
                "government_owned_lotteries_non_us_region",
                "government_owned_lotteries_us_region_only",
                "government_services",
                "grocery_stores_supermarkets",
                "hardware_equipment_and_supplies",
                "hardware_stores",
                "health_and_beauty_spas",
                "hearing_aids_sales_and_supplies",
                "heating_plumbing_a_c",
                "hobby_toy_and_game_shops",
                "home_supply_warehouse_stores",
                "hospitals",
                "hotels_motels_and_resorts",
                "household_appliance_stores",
                "industrial_supplies",
                "information_retrieval_services",
                "insurance_default",
                "insurance_underwriting_premiums",
                "intra_company_purchases",
                "jewelry_stores_watches_clocks_and_silverware_stores",
                "landscaping_services",
                "laundries",
                "laundry_cleaning_services",
                "legal_services_attorneys",
                "luggage_and_leather_goods_stores",
                "lumber_building_materials_stores",
                "manual_cash_disburse",
                "marinas_service_and_supplies",
                "marketplaces",
                "masonry_stonework_and_plaster",
                "massage_parlors",
                "medical_and_dental_labs",
                "medical_dental_ophthalmic_and_hospital_equipment_and_supplies",
                "medical_services",
                "membership_organizations",
                "mens_and_boys_clothing_and_accessories_stores",
                "mens_womens_clothing_stores",
                "metal_service_centers",
                "miscellaneous_apparel_and_accessory_shops",
                "miscellaneous_auto_dealers",
                "miscellaneous_business_services",
                "miscellaneous_food_stores",
                "miscellaneous_general_merchandise",
                "miscellaneous_general_services",
                "miscellaneous_home_furnishing_specialty_stores",
                "miscellaneous_publishing_and_printing",
                "miscellaneous_recreation_services",
                "miscellaneous_repair_shops",
                "miscellaneous_specialty_retail",
                "mobile_home_dealers",
                "motion_picture_theaters",
                "motor_freight_carriers_and_trucking",
                "motor_homes_dealers",
                "motor_vehicle_supplies_and_new_parts",
                "motorcycle_shops_and_dealers",
                "motorcycle_shops_dealers",
                "music_stores_musical_instruments_pianos_and_sheet_music",
                "news_dealers_and_newsstands",
                "non_fi_money_orders",
                "non_fi_stored_value_card_purchase_load",
                "nondurable_goods",
                "nurseries_lawn_and_garden_supply_stores",
                "nursing_personal_care",
                "office_and_commercial_furniture",
                "opticians_eyeglasses",
                "optometrists_ophthalmologist",
                "orthopedic_goods_prosthetic_devices",
                "osteopaths",
                "package_stores_beer_wine_and_liquor",
                "paints_varnishes_and_supplies",
                "parking_lots_garages",
                "passenger_railways",
                "pawn_shops",
                "pet_shops_pet_food_and_supplies",
                "petroleum_and_petroleum_products",
                "photo_developing",
                "photographic_photocopy_microfilm_equipment_and_supplies",
                "photographic_studios",
                "picture_video_production",
                "piece_goods_notions_and_other_dry_goods",
                "plumbing_heating_equipment_and_supplies",
                "political_organizations",
                "postal_services_government_only",
                "precious_stones_and_metals_watches_and_jewelry",
                "professional_services",
                "public_warehousing_and_storage",
                "quick_copy_repro_and_blueprint",
                "railroads",
                "real_estate_agents_and_managers_rentals",
                "record_stores",
                "recreational_vehicle_rentals",
                "religious_goods_stores",
                "religious_organizations",
                "roofing_siding_sheet_metal",
                "secretarial_support_services",
                "security_brokers_dealers",
                "service_stations",
                "sewing_needlework_fabric_and_piece_goods_stores",
                "shoe_repair_hat_cleaning",
                "shoe_stores",
                "small_appliance_repair",
                "snowmobile_dealers",
                "special_trade_services",
                "specialty_cleaning",
                "sporting_goods_stores",
                "sporting_recreation_camps",
                "sports_and_riding_apparel_stores",
                "sports_clubs_fields",
                "stamp_and_coin_stores",
                "stationary_office_supplies_printing_and_writing_paper",
                "stationery_stores_office_and_school_supply_stores",
                "swimming_pools_sales",
                "t_ui_travel_germany",
                "tailors_alterations",
                "tax_payments_government_agencies",
                "tax_preparation_services",
                "taxicabs_limousines",
                "telecommunication_equipment_and_telephone_sales",
                "telecommunication_services",
                "telegraph_services",
                "tent_and_awning_shops",
                "testing_laboratories",
                "theatrical_ticket_agencies",
                "timeshares",
                "tire_retreading_and_repair",
                "tolls_bridge_fees",
                "tourist_attractions_and_exhibits",
                "towing_services",
                "trailer_parks_campgrounds",
                "transportation_services",
                "travel_agencies_tour_operators",
                "truck_stop_iteration",
                "truck_utility_trailer_rentals",
                "typesetting_plate_making_and_related_services",
                "typewriter_stores",
                "u_s_federal_government_agencies_or_departments",
                "uniforms_commercial_clothing",
                "used_merchandise_and_secondhand_stores",
                "utilities",
                "variety_stores",
                "veterinary_services",
                "video_amusement_game_supplies",
                "video_game_arcades",
                "video_tape_rental_stores",
                "vocational_trade_schools",
                "watch_jewelry_repair",
                "welding_repair",
                "wholesale_clubs",
                "wig_and_toupee_stores",
                "wires_money_orders",
                "womens_accessory_and_specialty_shops",
                "womens_ready_to_wear_stores",
                "wrecking_and_salvage_yards",
            ]
        ]
        """
        A categorization of the seller's type of business. See our [merchant categories guide](https://stripe.com/docs/issuing/merchant-categories) for a list of possible values.
        """
        city: NotRequired[str]
        """
        City where the seller is located
        """
        country: NotRequired[str]
        """
        Country where the seller is located
        """
        name: NotRequired[str]
        """
        Name of the seller
        """
        network_id: NotRequired[str]
        """
        Identifier assigned to the seller by the card network. Different card networks may assign different network_id fields to the same merchant.
        """
        postal_code: NotRequired[str]
        """
        Postal code where the seller is located
        """
        state: NotRequired[str]
        """
        State where the seller is located
        """
        terminal_id: NotRequired[str]
        """
        An ID assigned by the seller to the location of the sale.
        """
        url: NotRequired[str]
        """
        URL provided by the merchant on a 3DS request
        """

    class CreateParamsNetworkData(TypedDict):
        acquiring_institution_id: NotRequired[str]
        """
        Identifier assigned to the acquirer by the card network.
        """

    class CreateParamsVerificationData(TypedDict):
        address_line1_check: NotRequired[
            Literal["match", "mismatch", "not_provided"]
        ]
        """
        Whether the cardholder provided an address first line and if it matched the cardholder's `billing.address.line1`.
        """
        address_postal_code_check: NotRequired[
            Literal["match", "mismatch", "not_provided"]
        ]
        """
        Whether the cardholder provided a postal code and if it matched the cardholder's `billing.address.postal_code`.
        """
        authentication_exemption: NotRequired[
            "Authorization.CreateParamsVerificationDataAuthenticationExemption"
        ]
        """
        The exemption applied to this authorization.
        """
        cvc_check: NotRequired[Literal["match", "mismatch", "not_provided"]]
        """
        Whether the cardholder provided a CVC and if it matched Stripe's record.
        """
        expiry_check: NotRequired[Literal["match", "mismatch", "not_provided"]]
        """
        Whether the cardholder provided an expiry date and if it matched Stripe's record.
        """
        three_d_secure: NotRequired[
            "Authorization.CreateParamsVerificationDataThreeDSecure"
        ]
        """
        3D Secure details.
        """

    class CreateParamsVerificationDataAuthenticationExemption(TypedDict):
        claimed_by: Literal["acquirer", "issuer"]
        """
        The entity that requested the exemption, either the acquiring merchant or the Issuing user.
        """
        type: Literal[
            "low_value_transaction", "transaction_risk_analysis", "unknown"
        ]
        """
        The specific exemption claimed for this authorization.
        """

    class CreateParamsVerificationDataThreeDSecure(TypedDict):
        result: Literal[
            "attempt_acknowledged", "authenticated", "failed", "required"
        ]
        """
        The outcome of the 3D Secure authentication request.
        """

    class DeclineParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """

    class ExpireParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class FinalizeAmountParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        final_amount: int
        """
        The final authorization amount that will be captured by the merchant. This amount is in the authorization currency and in the [smallest currency unit](https://stripe.com/docs/currencies#zero-decimal).
        """
        fleet: NotRequired["Authorization.FinalizeAmountParamsFleet"]
        """
        Fleet-specific information for authorizations using Fleet cards.
        """
        fuel: NotRequired["Authorization.FinalizeAmountParamsFuel"]
        """
        Information about fuel that was purchased with this transaction.
        """

    class FinalizeAmountParamsFleet(TypedDict):
        cardholder_prompt_data: NotRequired[
            "Authorization.FinalizeAmountParamsFleetCardholderPromptData"
        ]
        """
        Answers to prompts presented to the cardholder at the point of sale. Prompted fields vary depending on the configuration of your physical fleet cards. Typical points of sale support only numeric entry.
        """
        purchase_type: NotRequired[
            Literal[
                "fuel_and_non_fuel_purchase",
                "fuel_purchase",
                "non_fuel_purchase",
            ]
        ]
        """
        The type of purchase. One of `fuel_purchase`, `non_fuel_purchase`, or `fuel_and_non_fuel_purchase`.
        """
        reported_breakdown: NotRequired[
            "Authorization.FinalizeAmountParamsFleetReportedBreakdown"
        ]
        """
        More information about the total amount. This information is not guaranteed to be accurate as some merchants may provide unreliable data.
        """
        service_type: NotRequired[
            Literal["full_service", "non_fuel_transaction", "self_service"]
        ]
        """
        The type of fuel service. One of `non_fuel_transaction`, `full_service`, or `self_service`.
        """

    class FinalizeAmountParamsFleetCardholderPromptData(TypedDict):
        driver_id: NotRequired[str]
        """
        Driver ID.
        """
        odometer: NotRequired[int]
        """
        Odometer reading.
        """
        unspecified_id: NotRequired[str]
        """
        An alphanumeric ID. This field is used when a vehicle ID, driver ID, or generic ID is entered by the cardholder, but the merchant or card network did not specify the prompt type.
        """
        user_id: NotRequired[str]
        """
        User ID.
        """
        vehicle_number: NotRequired[str]
        """
        Vehicle number.
        """

    class FinalizeAmountParamsFleetReportedBreakdown(TypedDict):
        fuel: NotRequired[
            "Authorization.FinalizeAmountParamsFleetReportedBreakdownFuel"
        ]
        """
        Breakdown of fuel portion of the purchase.
        """
        non_fuel: NotRequired[
            "Authorization.FinalizeAmountParamsFleetReportedBreakdownNonFuel"
        ]
        """
        Breakdown of non-fuel portion of the purchase.
        """
        tax: NotRequired[
            "Authorization.FinalizeAmountParamsFleetReportedBreakdownTax"
        ]
        """
        Information about tax included in this transaction.
        """

    class FinalizeAmountParamsFleetReportedBreakdownFuel(TypedDict):
        gross_amount_decimal: NotRequired[str]
        """
        Gross fuel amount that should equal Fuel Volume multipled by Fuel Unit Cost, inclusive of taxes.
        """

    class FinalizeAmountParamsFleetReportedBreakdownNonFuel(TypedDict):
        gross_amount_decimal: NotRequired[str]
        """
        Gross non-fuel amount that should equal the sum of the line items, inclusive of taxes.
        """

    class FinalizeAmountParamsFleetReportedBreakdownTax(TypedDict):
        local_amount_decimal: NotRequired[str]
        """
        Amount of state or provincial Sales Tax included in the transaction amount. Null if not reported by merchant or not subject to tax.
        """
        national_amount_decimal: NotRequired[str]
        """
        Amount of national Sales Tax or VAT included in the transaction amount. Null if not reported by merchant or not subject to tax.
        """

    class FinalizeAmountParamsFuel(TypedDict):
        industry_product_code: NotRequired[str]
        """
        [Conexxus Payment System Product Code](https://www.conexxus.org/conexxus-payment-system-product-codes) identifying the primary fuel product purchased.
        """
        quantity_decimal: NotRequired[str]
        """
        The quantity of `unit`s of fuel that was dispensed, represented as a decimal string with at most 12 decimal places.
        """
        type: NotRequired[
            Literal[
                "diesel",
                "other",
                "unleaded_plus",
                "unleaded_regular",
                "unleaded_super",
            ]
        ]
        """
        The type of fuel that was purchased. One of `diesel`, `unleaded_plus`, `unleaded_regular`, `unleaded_super`, or `other`.
        """
        unit: NotRequired[
            Literal[
                "charging_minute",
                "imperial_gallon",
                "kilogram",
                "kilowatt_hour",
                "liter",
                "other",
                "pound",
                "us_gallon",
            ]
        ]
        """
        The units for `quantity_decimal`. One of `charging_minute`, `imperial_gallon`, `kilogram`, `kilowatt_hour`, `liter`, `pound`, `us_gallon`, or `other`.
        """
        unit_cost_decimal: NotRequired[str]
        """
        The cost in cents per each unit of fuel, represented as a decimal string with at most 12 decimal places.
        """

    class IncrementParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        increment_amount: int
        """
        The amount to increment the authorization by. This amount is in the authorization currency and in the [smallest currency unit](https://stripe.com/docs/currencies#zero-decimal).
        """
        is_amount_controllable: NotRequired[bool]
        """
        If set `true`, you may provide [amount](https://stripe.com/docs/api/issuing/authorizations/approve#approve_issuing_authorization-amount) to control how much to hold for the authorization.
        """

    class ListParams(RequestOptions):
        card: NotRequired[str]
        """
        Only return authorizations that belong to the given card.
        """
        cardholder: NotRequired[str]
        """
        Only return authorizations that belong to the given cardholder.
        """
        created: NotRequired["Authorization.ListParamsCreated|int"]
        """
        Only return authorizations that were created during the given date interval.
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
        status: NotRequired[Literal["closed", "pending", "reversed"]]
        """
        Only return authorizations with the given status. One of `pending`, `closed`, or `reversed`.
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
        reverse_amount: NotRequired[int]
        """
        The amount to reverse from the authorization. If not provided, the full amount of the authorization will be reversed. This amount is in the authorization currency and in the [smallest currency unit](https://stripe.com/docs/currencies#zero-decimal).
        """

    amount: int
    """
    The total amount that was authorized or rejected. This amount is in `currency` and in the [smallest currency unit](https://stripe.com/docs/currencies#zero-decimal). `amount` should be the same as `merchant_amount`, unless `currency` and `merchant_currency` are different.
    """
    amount_details: Optional[AmountDetails]
    """
    Detailed breakdown of amount components. These amounts are denominated in `currency` and in the [smallest currency unit](https://stripe.com/docs/currencies#zero-decimal).
    """
    approved: bool
    """
    Whether the authorization has been approved.
    """
    authorization_method: Literal[
        "chip", "contactless", "keyed_in", "online", "swipe"
    ]
    """
    How the card details were provided.
    """
    balance_transactions: List["BalanceTransaction"]
    """
    List of balance transactions associated with this authorization.
    """
    card: "Card"
    """
    You can [create physical or virtual cards](https://stripe.com/docs/issuing/cards) that are issued to cardholders.
    """
    cardholder: Optional[ExpandableField["Cardholder"]]
    """
    The cardholder to whom this authorization belongs.
    """
    created: int
    """
    Time at which the object was created. Measured in seconds since the Unix epoch.
    """
    currency: str
    """
    The currency of the cardholder. This currency can be different from the currency presented at authorization and the `merchant_currency` field on this authorization. Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
    """
    fleet: Optional[Fleet]
    """
    Fleet-specific information for authorizations using Fleet cards.
    """
    fuel: Optional[Fuel]
    """
    Information about fuel that was purchased with this transaction. Typically this information is received from the merchant after the authorization has been approved and the fuel dispensed.
    """
    id: str
    """
    Unique identifier for the object.
    """
    livemode: bool
    """
    Has the value `true` if the object exists in live mode or the value `false` if the object exists in test mode.
    """
    merchant_amount: int
    """
    The total amount that was authorized or rejected. This amount is in the `merchant_currency` and in the [smallest currency unit](https://stripe.com/docs/currencies#zero-decimal). `merchant_amount` should be the same as `amount`, unless `merchant_currency` and `currency` are different.
    """
    merchant_currency: str
    """
    The local currency that was presented to the cardholder for the authorization. This currency can be different from the cardholder currency and the `currency` field on this authorization. Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
    """
    merchant_data: MerchantData
    metadata: Dict[str, str]
    """
    Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format.
    """
    network_data: Optional[NetworkData]
    """
    Details about the authorization, such as identifiers, set by the card network.
    """
    object: Literal["issuing.authorization"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """
    pending_request: Optional[PendingRequest]
    """
    The pending authorization request. This field will only be non-null during an `issuing_authorization.request` webhook.
    """
    request_history: List[RequestHistory]
    """
    History of every time a `pending_request` authorization was approved/declined, either by you directly or by Stripe (e.g. based on your spending_controls). If the merchant changes the authorization by performing an incremental authorization, you can look at this field to see the previous requests for the authorization. This field can be helpful in determining why a given authorization was approved/declined.
    """
    status: Literal["closed", "pending", "reversed"]
    """
    The current status of the authorization in its lifecycle.
    """
    token: Optional[ExpandableField["Token"]]
    """
    [Token](https://stripe.com/docs/api/issuing/tokens/object) object used for this authorization. If a network token was not used for this authorization, this field will be null.
    """
    transactions: List["Transaction"]
    """
    List of [transactions](https://stripe.com/docs/api/issuing/transactions) associated with this authorization.
    """
    treasury: Optional[Treasury]
    """
    [Treasury](https://stripe.com/docs/api/treasury) details related to this authorization if it was created on a [FinancialAccount](https://stripe.com/docs/api/treasury/financial_accounts).
    """
    verification_data: VerificationData
    wallet: Optional[str]
    """
    The digital wallet used for this transaction. One of `apple_pay`, `google_pay`, or `samsung_pay`. Will populate as `null` when no digital wallet was utilized.
    """

    @classmethod
    def _cls_approve(
        cls,
        authorization: str,
        **params: Unpack["Authorization.ApproveParams"],
    ) -> "Authorization":
        """
        [Deprecated] Approves a pending Issuing Authorization object. This request should be made within the timeout window of the [real-time authorization](https://stripe.com/docs/issuing/controls/real-time-authorizations) flow.
        This method is deprecated. Instead, [respond directly to the webhook request to approve an authorization](https://stripe.com/docs/issuing/controls/real-time-authorizations#authorization-handling).
        """
        return cast(
            "Authorization",
            cls._static_request(
                "post",
                "/v1/issuing/authorizations/{authorization}/approve".format(
                    authorization=sanitize_id(authorization)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    def approve(
        authorization: str, **params: Unpack["Authorization.ApproveParams"]
    ) -> "Authorization":
        """
        [Deprecated] Approves a pending Issuing Authorization object. This request should be made within the timeout window of the [real-time authorization](https://stripe.com/docs/issuing/controls/real-time-authorizations) flow.
        This method is deprecated. Instead, [respond directly to the webhook request to approve an authorization](https://stripe.com/docs/issuing/controls/real-time-authorizations#authorization-handling).
        """
        ...

    @overload
    def approve(
        self, **params: Unpack["Authorization.ApproveParams"]
    ) -> "Authorization":
        """
        [Deprecated] Approves a pending Issuing Authorization object. This request should be made within the timeout window of the [real-time authorization](https://stripe.com/docs/issuing/controls/real-time-authorizations) flow.
        This method is deprecated. Instead, [respond directly to the webhook request to approve an authorization](https://stripe.com/docs/issuing/controls/real-time-authorizations#authorization-handling).
        """
        ...

    @class_method_variant("_cls_approve")
    def approve(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Authorization.ApproveParams"]
    ) -> "Authorization":
        """
        [Deprecated] Approves a pending Issuing Authorization object. This request should be made within the timeout window of the [real-time authorization](https://stripe.com/docs/issuing/controls/real-time-authorizations) flow.
        This method is deprecated. Instead, [respond directly to the webhook request to approve an authorization](https://stripe.com/docs/issuing/controls/real-time-authorizations#authorization-handling).
        """
        return cast(
            "Authorization",
            self._request(
                "post",
                "/v1/issuing/authorizations/{authorization}/approve".format(
                    authorization=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    async def _cls_approve_async(
        cls,
        authorization: str,
        **params: Unpack["Authorization.ApproveParams"],
    ) -> "Authorization":
        """
        [Deprecated] Approves a pending Issuing Authorization object. This request should be made within the timeout window of the [real-time authorization](https://stripe.com/docs/issuing/controls/real-time-authorizations) flow.
        This method is deprecated. Instead, [respond directly to the webhook request to approve an authorization](https://stripe.com/docs/issuing/controls/real-time-authorizations#authorization-handling).
        """
        return cast(
            "Authorization",
            await cls._static_request_async(
                "post",
                "/v1/issuing/authorizations/{authorization}/approve".format(
                    authorization=sanitize_id(authorization)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    async def approve_async(
        authorization: str, **params: Unpack["Authorization.ApproveParams"]
    ) -> "Authorization":
        """
        [Deprecated] Approves a pending Issuing Authorization object. This request should be made within the timeout window of the [real-time authorization](https://stripe.com/docs/issuing/controls/real-time-authorizations) flow.
        This method is deprecated. Instead, [respond directly to the webhook request to approve an authorization](https://stripe.com/docs/issuing/controls/real-time-authorizations#authorization-handling).
        """
        ...

    @overload
    async def approve_async(
        self, **params: Unpack["Authorization.ApproveParams"]
    ) -> "Authorization":
        """
        [Deprecated] Approves a pending Issuing Authorization object. This request should be made within the timeout window of the [real-time authorization](https://stripe.com/docs/issuing/controls/real-time-authorizations) flow.
        This method is deprecated. Instead, [respond directly to the webhook request to approve an authorization](https://stripe.com/docs/issuing/controls/real-time-authorizations#authorization-handling).
        """
        ...

    @class_method_variant("_cls_approve_async")
    async def approve_async(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Authorization.ApproveParams"]
    ) -> "Authorization":
        """
        [Deprecated] Approves a pending Issuing Authorization object. This request should be made within the timeout window of the [real-time authorization](https://stripe.com/docs/issuing/controls/real-time-authorizations) flow.
        This method is deprecated. Instead, [respond directly to the webhook request to approve an authorization](https://stripe.com/docs/issuing/controls/real-time-authorizations#authorization-handling).
        """
        return cast(
            "Authorization",
            await self._request_async(
                "post",
                "/v1/issuing/authorizations/{authorization}/approve".format(
                    authorization=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    def _cls_decline(
        cls,
        authorization: str,
        **params: Unpack["Authorization.DeclineParams"],
    ) -> "Authorization":
        """
        [Deprecated] Declines a pending Issuing Authorization object. This request should be made within the timeout window of the [real time authorization](https://stripe.com/docs/issuing/controls/real-time-authorizations) flow.
        This method is deprecated. Instead, [respond directly to the webhook request to decline an authorization](https://stripe.com/docs/issuing/controls/real-time-authorizations#authorization-handling).
        """
        return cast(
            "Authorization",
            cls._static_request(
                "post",
                "/v1/issuing/authorizations/{authorization}/decline".format(
                    authorization=sanitize_id(authorization)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    def decline(
        authorization: str, **params: Unpack["Authorization.DeclineParams"]
    ) -> "Authorization":
        """
        [Deprecated] Declines a pending Issuing Authorization object. This request should be made within the timeout window of the [real time authorization](https://stripe.com/docs/issuing/controls/real-time-authorizations) flow.
        This method is deprecated. Instead, [respond directly to the webhook request to decline an authorization](https://stripe.com/docs/issuing/controls/real-time-authorizations#authorization-handling).
        """
        ...

    @overload
    def decline(
        self, **params: Unpack["Authorization.DeclineParams"]
    ) -> "Authorization":
        """
        [Deprecated] Declines a pending Issuing Authorization object. This request should be made within the timeout window of the [real time authorization](https://stripe.com/docs/issuing/controls/real-time-authorizations) flow.
        This method is deprecated. Instead, [respond directly to the webhook request to decline an authorization](https://stripe.com/docs/issuing/controls/real-time-authorizations#authorization-handling).
        """
        ...

    @class_method_variant("_cls_decline")
    def decline(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Authorization.DeclineParams"]
    ) -> "Authorization":
        """
        [Deprecated] Declines a pending Issuing Authorization object. This request should be made within the timeout window of the [real time authorization](https://stripe.com/docs/issuing/controls/real-time-authorizations) flow.
        This method is deprecated. Instead, [respond directly to the webhook request to decline an authorization](https://stripe.com/docs/issuing/controls/real-time-authorizations#authorization-handling).
        """
        return cast(
            "Authorization",
            self._request(
                "post",
                "/v1/issuing/authorizations/{authorization}/decline".format(
                    authorization=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    async def _cls_decline_async(
        cls,
        authorization: str,
        **params: Unpack["Authorization.DeclineParams"],
    ) -> "Authorization":
        """
        [Deprecated] Declines a pending Issuing Authorization object. This request should be made within the timeout window of the [real time authorization](https://stripe.com/docs/issuing/controls/real-time-authorizations) flow.
        This method is deprecated. Instead, [respond directly to the webhook request to decline an authorization](https://stripe.com/docs/issuing/controls/real-time-authorizations#authorization-handling).
        """
        return cast(
            "Authorization",
            await cls._static_request_async(
                "post",
                "/v1/issuing/authorizations/{authorization}/decline".format(
                    authorization=sanitize_id(authorization)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    async def decline_async(
        authorization: str, **params: Unpack["Authorization.DeclineParams"]
    ) -> "Authorization":
        """
        [Deprecated] Declines a pending Issuing Authorization object. This request should be made within the timeout window of the [real time authorization](https://stripe.com/docs/issuing/controls/real-time-authorizations) flow.
        This method is deprecated. Instead, [respond directly to the webhook request to decline an authorization](https://stripe.com/docs/issuing/controls/real-time-authorizations#authorization-handling).
        """
        ...

    @overload
    async def decline_async(
        self, **params: Unpack["Authorization.DeclineParams"]
    ) -> "Authorization":
        """
        [Deprecated] Declines a pending Issuing Authorization object. This request should be made within the timeout window of the [real time authorization](https://stripe.com/docs/issuing/controls/real-time-authorizations) flow.
        This method is deprecated. Instead, [respond directly to the webhook request to decline an authorization](https://stripe.com/docs/issuing/controls/real-time-authorizations#authorization-handling).
        """
        ...

    @class_method_variant("_cls_decline_async")
    async def decline_async(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Authorization.DeclineParams"]
    ) -> "Authorization":
        """
        [Deprecated] Declines a pending Issuing Authorization object. This request should be made within the timeout window of the [real time authorization](https://stripe.com/docs/issuing/controls/real-time-authorizations) flow.
        This method is deprecated. Instead, [respond directly to the webhook request to decline an authorization](https://stripe.com/docs/issuing/controls/real-time-authorizations#authorization-handling).
        """
        return cast(
            "Authorization",
            await self._request_async(
                "post",
                "/v1/issuing/authorizations/{authorization}/decline".format(
                    authorization=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    def list(
        cls, **params: Unpack["Authorization.ListParams"]
    ) -> ListObject["Authorization"]:
        """
        Returns a list of Issuing Authorization objects. The objects are sorted in descending order by creation date, with the most recently created object appearing first.
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
        cls, **params: Unpack["Authorization.ListParams"]
    ) -> ListObject["Authorization"]:
        """
        Returns a list of Issuing Authorization objects. The objects are sorted in descending order by creation date, with the most recently created object appearing first.
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
        cls, id: str, **params: Unpack["Authorization.ModifyParams"]
    ) -> "Authorization":
        """
        Updates the specified Issuing Authorization object by setting the values of the parameters passed. Any parameters not provided will be left unchanged.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(id))
        return cast(
            "Authorization",
            cls._static_request(
                "post",
                url,
                params=params,
            ),
        )

    @classmethod
    async def modify_async(
        cls, id: str, **params: Unpack["Authorization.ModifyParams"]
    ) -> "Authorization":
        """
        Updates the specified Issuing Authorization object by setting the values of the parameters passed. Any parameters not provided will be left unchanged.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(id))
        return cast(
            "Authorization",
            await cls._static_request_async(
                "post",
                url,
                params=params,
            ),
        )

    @classmethod
    def retrieve(
        cls, id: str, **params: Unpack["Authorization.RetrieveParams"]
    ) -> "Authorization":
        """
        Retrieves an Issuing Authorization object.
        """
        instance = cls(id, **params)
        instance.refresh()
        return instance

    @classmethod
    async def retrieve_async(
        cls, id: str, **params: Unpack["Authorization.RetrieveParams"]
    ) -> "Authorization":
        """
        Retrieves an Issuing Authorization object.
        """
        instance = cls(id, **params)
        await instance.refresh_async()
        return instance

    class TestHelpers(APIResourceTestHelpers["Authorization"]):
        _resource_cls: Type["Authorization"]

        @classmethod
        def _cls_capture(
            cls,
            authorization: str,
            **params: Unpack["Authorization.CaptureParams"],
        ) -> "Authorization":
            """
            Capture a test-mode authorization.
            """
            return cast(
                "Authorization",
                cls._static_request(
                    "post",
                    "/v1/test_helpers/issuing/authorizations/{authorization}/capture".format(
                        authorization=sanitize_id(authorization)
                    ),
                    params=params,
                ),
            )

        @overload
        @staticmethod
        def capture(
            authorization: str, **params: Unpack["Authorization.CaptureParams"]
        ) -> "Authorization":
            """
            Capture a test-mode authorization.
            """
            ...

        @overload
        def capture(
            self, **params: Unpack["Authorization.CaptureParams"]
        ) -> "Authorization":
            """
            Capture a test-mode authorization.
            """
            ...

        @class_method_variant("_cls_capture")
        def capture(  # pyright: ignore[reportGeneralTypeIssues]
            self, **params: Unpack["Authorization.CaptureParams"]
        ) -> "Authorization":
            """
            Capture a test-mode authorization.
            """
            return cast(
                "Authorization",
                self.resource._request(
                    "post",
                    "/v1/test_helpers/issuing/authorizations/{authorization}/capture".format(
                        authorization=sanitize_id(self.resource.get("id"))
                    ),
                    params=params,
                ),
            )

        @classmethod
        async def _cls_capture_async(
            cls,
            authorization: str,
            **params: Unpack["Authorization.CaptureParams"],
        ) -> "Authorization":
            """
            Capture a test-mode authorization.
            """
            return cast(
                "Authorization",
                await cls._static_request_async(
                    "post",
                    "/v1/test_helpers/issuing/authorizations/{authorization}/capture".format(
                        authorization=sanitize_id(authorization)
                    ),
                    params=params,
                ),
            )

        @overload
        @staticmethod
        async def capture_async(
            authorization: str, **params: Unpack["Authorization.CaptureParams"]
        ) -> "Authorization":
            """
            Capture a test-mode authorization.
            """
            ...

        @overload
        async def capture_async(
            self, **params: Unpack["Authorization.CaptureParams"]
        ) -> "Authorization":
            """
            Capture a test-mode authorization.
            """
            ...

        @class_method_variant("_cls_capture_async")
        async def capture_async(  # pyright: ignore[reportGeneralTypeIssues]
            self, **params: Unpack["Authorization.CaptureParams"]
        ) -> "Authorization":
            """
            Capture a test-mode authorization.
            """
            return cast(
                "Authorization",
                await self.resource._request_async(
                    "post",
                    "/v1/test_helpers/issuing/authorizations/{authorization}/capture".format(
                        authorization=sanitize_id(self.resource.get("id"))
                    ),
                    params=params,
                ),
            )

        @classmethod
        def create(
            cls, **params: Unpack["Authorization.CreateParams"]
        ) -> "Authorization":
            """
            Create a test-mode authorization.
            """
            return cast(
                "Authorization",
                cls._static_request(
                    "post",
                    "/v1/test_helpers/issuing/authorizations",
                    params=params,
                ),
            )

        @classmethod
        async def create_async(
            cls, **params: Unpack["Authorization.CreateParams"]
        ) -> "Authorization":
            """
            Create a test-mode authorization.
            """
            return cast(
                "Authorization",
                await cls._static_request_async(
                    "post",
                    "/v1/test_helpers/issuing/authorizations",
                    params=params,
                ),
            )

        @classmethod
        def _cls_expire(
            cls,
            authorization: str,
            **params: Unpack["Authorization.ExpireParams"],
        ) -> "Authorization":
            """
            Expire a test-mode Authorization.
            """
            return cast(
                "Authorization",
                cls._static_request(
                    "post",
                    "/v1/test_helpers/issuing/authorizations/{authorization}/expire".format(
                        authorization=sanitize_id(authorization)
                    ),
                    params=params,
                ),
            )

        @overload
        @staticmethod
        def expire(
            authorization: str, **params: Unpack["Authorization.ExpireParams"]
        ) -> "Authorization":
            """
            Expire a test-mode Authorization.
            """
            ...

        @overload
        def expire(
            self, **params: Unpack["Authorization.ExpireParams"]
        ) -> "Authorization":
            """
            Expire a test-mode Authorization.
            """
            ...

        @class_method_variant("_cls_expire")
        def expire(  # pyright: ignore[reportGeneralTypeIssues]
            self, **params: Unpack["Authorization.ExpireParams"]
        ) -> "Authorization":
            """
            Expire a test-mode Authorization.
            """
            return cast(
                "Authorization",
                self.resource._request(
                    "post",
                    "/v1/test_helpers/issuing/authorizations/{authorization}/expire".format(
                        authorization=sanitize_id(self.resource.get("id"))
                    ),
                    params=params,
                ),
            )

        @classmethod
        async def _cls_expire_async(
            cls,
            authorization: str,
            **params: Unpack["Authorization.ExpireParams"],
        ) -> "Authorization":
            """
            Expire a test-mode Authorization.
            """
            return cast(
                "Authorization",
                await cls._static_request_async(
                    "post",
                    "/v1/test_helpers/issuing/authorizations/{authorization}/expire".format(
                        authorization=sanitize_id(authorization)
                    ),
                    params=params,
                ),
            )

        @overload
        @staticmethod
        async def expire_async(
            authorization: str, **params: Unpack["Authorization.ExpireParams"]
        ) -> "Authorization":
            """
            Expire a test-mode Authorization.
            """
            ...

        @overload
        async def expire_async(
            self, **params: Unpack["Authorization.ExpireParams"]
        ) -> "Authorization":
            """
            Expire a test-mode Authorization.
            """
            ...

        @class_method_variant("_cls_expire_async")
        async def expire_async(  # pyright: ignore[reportGeneralTypeIssues]
            self, **params: Unpack["Authorization.ExpireParams"]
        ) -> "Authorization":
            """
            Expire a test-mode Authorization.
            """
            return cast(
                "Authorization",
                await self.resource._request_async(
                    "post",
                    "/v1/test_helpers/issuing/authorizations/{authorization}/expire".format(
                        authorization=sanitize_id(self.resource.get("id"))
                    ),
                    params=params,
                ),
            )

        @classmethod
        def _cls_finalize_amount(
            cls,
            authorization: str,
            **params: Unpack["Authorization.FinalizeAmountParams"],
        ) -> "Authorization":
            """
            Finalize the amount on an Authorization prior to capture, when the initial authorization was for an estimated amount.
            """
            return cast(
                "Authorization",
                cls._static_request(
                    "post",
                    "/v1/test_helpers/issuing/authorizations/{authorization}/finalize_amount".format(
                        authorization=sanitize_id(authorization)
                    ),
                    params=params,
                ),
            )

        @overload
        @staticmethod
        def finalize_amount(
            authorization: str,
            **params: Unpack["Authorization.FinalizeAmountParams"],
        ) -> "Authorization":
            """
            Finalize the amount on an Authorization prior to capture, when the initial authorization was for an estimated amount.
            """
            ...

        @overload
        def finalize_amount(
            self, **params: Unpack["Authorization.FinalizeAmountParams"]
        ) -> "Authorization":
            """
            Finalize the amount on an Authorization prior to capture, when the initial authorization was for an estimated amount.
            """
            ...

        @class_method_variant("_cls_finalize_amount")
        def finalize_amount(  # pyright: ignore[reportGeneralTypeIssues]
            self, **params: Unpack["Authorization.FinalizeAmountParams"]
        ) -> "Authorization":
            """
            Finalize the amount on an Authorization prior to capture, when the initial authorization was for an estimated amount.
            """
            return cast(
                "Authorization",
                self.resource._request(
                    "post",
                    "/v1/test_helpers/issuing/authorizations/{authorization}/finalize_amount".format(
                        authorization=sanitize_id(self.resource.get("id"))
                    ),
                    params=params,
                ),
            )

        @classmethod
        async def _cls_finalize_amount_async(
            cls,
            authorization: str,
            **params: Unpack["Authorization.FinalizeAmountParams"],
        ) -> "Authorization":
            """
            Finalize the amount on an Authorization prior to capture, when the initial authorization was for an estimated amount.
            """
            return cast(
                "Authorization",
                await cls._static_request_async(
                    "post",
                    "/v1/test_helpers/issuing/authorizations/{authorization}/finalize_amount".format(
                        authorization=sanitize_id(authorization)
                    ),
                    params=params,
                ),
            )

        @overload
        @staticmethod
        async def finalize_amount_async(
            authorization: str,
            **params: Unpack["Authorization.FinalizeAmountParams"],
        ) -> "Authorization":
            """
            Finalize the amount on an Authorization prior to capture, when the initial authorization was for an estimated amount.
            """
            ...

        @overload
        async def finalize_amount_async(
            self, **params: Unpack["Authorization.FinalizeAmountParams"]
        ) -> "Authorization":
            """
            Finalize the amount on an Authorization prior to capture, when the initial authorization was for an estimated amount.
            """
            ...

        @class_method_variant("_cls_finalize_amount_async")
        async def finalize_amount_async(  # pyright: ignore[reportGeneralTypeIssues]
            self, **params: Unpack["Authorization.FinalizeAmountParams"]
        ) -> "Authorization":
            """
            Finalize the amount on an Authorization prior to capture, when the initial authorization was for an estimated amount.
            """
            return cast(
                "Authorization",
                await self.resource._request_async(
                    "post",
                    "/v1/test_helpers/issuing/authorizations/{authorization}/finalize_amount".format(
                        authorization=sanitize_id(self.resource.get("id"))
                    ),
                    params=params,
                ),
            )

        @classmethod
        def _cls_increment(
            cls,
            authorization: str,
            **params: Unpack["Authorization.IncrementParams"],
        ) -> "Authorization":
            """
            Increment a test-mode Authorization.
            """
            return cast(
                "Authorization",
                cls._static_request(
                    "post",
                    "/v1/test_helpers/issuing/authorizations/{authorization}/increment".format(
                        authorization=sanitize_id(authorization)
                    ),
                    params=params,
                ),
            )

        @overload
        @staticmethod
        def increment(
            authorization: str,
            **params: Unpack["Authorization.IncrementParams"],
        ) -> "Authorization":
            """
            Increment a test-mode Authorization.
            """
            ...

        @overload
        def increment(
            self, **params: Unpack["Authorization.IncrementParams"]
        ) -> "Authorization":
            """
            Increment a test-mode Authorization.
            """
            ...

        @class_method_variant("_cls_increment")
        def increment(  # pyright: ignore[reportGeneralTypeIssues]
            self, **params: Unpack["Authorization.IncrementParams"]
        ) -> "Authorization":
            """
            Increment a test-mode Authorization.
            """
            return cast(
                "Authorization",
                self.resource._request(
                    "post",
                    "/v1/test_helpers/issuing/authorizations/{authorization}/increment".format(
                        authorization=sanitize_id(self.resource.get("id"))
                    ),
                    params=params,
                ),
            )

        @classmethod
        async def _cls_increment_async(
            cls,
            authorization: str,
            **params: Unpack["Authorization.IncrementParams"],
        ) -> "Authorization":
            """
            Increment a test-mode Authorization.
            """
            return cast(
                "Authorization",
                await cls._static_request_async(
                    "post",
                    "/v1/test_helpers/issuing/authorizations/{authorization}/increment".format(
                        authorization=sanitize_id(authorization)
                    ),
                    params=params,
                ),
            )

        @overload
        @staticmethod
        async def increment_async(
            authorization: str,
            **params: Unpack["Authorization.IncrementParams"],
        ) -> "Authorization":
            """
            Increment a test-mode Authorization.
            """
            ...

        @overload
        async def increment_async(
            self, **params: Unpack["Authorization.IncrementParams"]
        ) -> "Authorization":
            """
            Increment a test-mode Authorization.
            """
            ...

        @class_method_variant("_cls_increment_async")
        async def increment_async(  # pyright: ignore[reportGeneralTypeIssues]
            self, **params: Unpack["Authorization.IncrementParams"]
        ) -> "Authorization":
            """
            Increment a test-mode Authorization.
            """
            return cast(
                "Authorization",
                await self.resource._request_async(
                    "post",
                    "/v1/test_helpers/issuing/authorizations/{authorization}/increment".format(
                        authorization=sanitize_id(self.resource.get("id"))
                    ),
                    params=params,
                ),
            )

        @classmethod
        def _cls_reverse(
            cls,
            authorization: str,
            **params: Unpack["Authorization.ReverseParams"],
        ) -> "Authorization":
            """
            Reverse a test-mode Authorization.
            """
            return cast(
                "Authorization",
                cls._static_request(
                    "post",
                    "/v1/test_helpers/issuing/authorizations/{authorization}/reverse".format(
                        authorization=sanitize_id(authorization)
                    ),
                    params=params,
                ),
            )

        @overload
        @staticmethod
        def reverse(
            authorization: str, **params: Unpack["Authorization.ReverseParams"]
        ) -> "Authorization":
            """
            Reverse a test-mode Authorization.
            """
            ...

        @overload
        def reverse(
            self, **params: Unpack["Authorization.ReverseParams"]
        ) -> "Authorization":
            """
            Reverse a test-mode Authorization.
            """
            ...

        @class_method_variant("_cls_reverse")
        def reverse(  # pyright: ignore[reportGeneralTypeIssues]
            self, **params: Unpack["Authorization.ReverseParams"]
        ) -> "Authorization":
            """
            Reverse a test-mode Authorization.
            """
            return cast(
                "Authorization",
                self.resource._request(
                    "post",
                    "/v1/test_helpers/issuing/authorizations/{authorization}/reverse".format(
                        authorization=sanitize_id(self.resource.get("id"))
                    ),
                    params=params,
                ),
            )

        @classmethod
        async def _cls_reverse_async(
            cls,
            authorization: str,
            **params: Unpack["Authorization.ReverseParams"],
        ) -> "Authorization":
            """
            Reverse a test-mode Authorization.
            """
            return cast(
                "Authorization",
                await cls._static_request_async(
                    "post",
                    "/v1/test_helpers/issuing/authorizations/{authorization}/reverse".format(
                        authorization=sanitize_id(authorization)
                    ),
                    params=params,
                ),
            )

        @overload
        @staticmethod
        async def reverse_async(
            authorization: str, **params: Unpack["Authorization.ReverseParams"]
        ) -> "Authorization":
            """
            Reverse a test-mode Authorization.
            """
            ...

        @overload
        async def reverse_async(
            self, **params: Unpack["Authorization.ReverseParams"]
        ) -> "Authorization":
            """
            Reverse a test-mode Authorization.
            """
            ...

        @class_method_variant("_cls_reverse_async")
        async def reverse_async(  # pyright: ignore[reportGeneralTypeIssues]
            self, **params: Unpack["Authorization.ReverseParams"]
        ) -> "Authorization":
            """
            Reverse a test-mode Authorization.
            """
            return cast(
                "Authorization",
                await self.resource._request_async(
                    "post",
                    "/v1/test_helpers/issuing/authorizations/{authorization}/reverse".format(
                        authorization=sanitize_id(self.resource.get("id"))
                    ),
                    params=params,
                ),
            )

    @property
    def test_helpers(self):
        return self.TestHelpers(self)

    _inner_class_types = {
        "amount_details": AmountDetails,
        "fleet": Fleet,
        "fuel": Fuel,
        "merchant_data": MerchantData,
        "network_data": NetworkData,
        "pending_request": PendingRequest,
        "request_history": RequestHistory,
        "treasury": Treasury,
        "verification_data": VerificationData,
    }


Authorization.TestHelpers._resource_cls = Authorization

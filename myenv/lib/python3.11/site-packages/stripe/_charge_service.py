# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._charge import Charge
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._search_result_object import SearchResultObject
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from typing import Dict, List, Union, cast
from typing_extensions import Literal, NotRequired, TypedDict


class ChargeService(StripeService):
    class CaptureParams(TypedDict):
        amount: NotRequired[int]
        """
        The amount to capture, which must be less than or equal to the original amount. Any additional amount will be automatically refunded.
        """
        application_fee: NotRequired[int]
        """
        An application fee to add on to this charge.
        """
        application_fee_amount: NotRequired[int]
        """
        An application fee amount to add on to this charge, which must be less than or equal to the original amount.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        receipt_email: NotRequired[str]
        """
        The email address to send this charge's receipt to. This will override the previously-specified email address for this charge, if one was set. Receipts will not be sent in test mode.
        """
        statement_descriptor: NotRequired[str]
        """
        For a non-card charge, text that appears on the customer's statement as the [statement descriptor](https://docs.stripe.com/get-started/account/statement-descriptors). This value overrides the account's default statement descriptor. For a card charge, this value is ignored unless you don't specify a `statement_descriptor_suffix`, in which case this value is used as the suffix.
        """
        statement_descriptor_suffix: NotRequired[str]
        """
        Provides information about a card charge. Concatenated to the account's [statement descriptor prefix](https://docs.corp.stripe.com/get-started/account/statement-descriptors#static) to form the complete statement descriptor that appears on the customer's statement. If the account has no prefix value, the suffix is concatenated to the account's statement descriptor.
        """
        transfer_data: NotRequired["ChargeService.CaptureParamsTransferData"]
        """
        An optional dictionary including the account to automatically transfer to as part of a destination charge. [See the Connect documentation](https://stripe.com/docs/connect/destination-charges) for details.
        """
        transfer_group: NotRequired[str]
        """
        A string that identifies this transaction as part of a group. `transfer_group` may only be provided if it has not been set. See the [Connect documentation](https://stripe.com/docs/connect/separate-charges-and-transfers#transfer-options) for details.
        """

    class CaptureParamsTransferData(TypedDict):
        amount: NotRequired[int]
        """
        The amount transferred to the destination account, if specified. By default, the entire charge amount is transferred to the destination account.
        """

    class CreateParams(TypedDict):
        amount: NotRequired[int]
        """
        Amount intended to be collected by this payment. A positive integer representing how much to charge in the [smallest currency unit](https://stripe.com/docs/currencies#zero-decimal) (e.g., 100 cents to charge $1.00 or 100 to charge ¥100, a zero-decimal currency). The minimum amount is $0.50 US or [equivalent in charge currency](https://stripe.com/docs/currencies#minimum-and-maximum-charge-amounts). The amount value supports up to eight digits (e.g., a value of 99999999 for a USD charge of $999,999.99).
        """
        application_fee: NotRequired[int]
        application_fee_amount: NotRequired[int]
        """
        A fee in cents (or local equivalent) that will be applied to the charge and transferred to the application owner's Stripe account. The request must be made with an OAuth key or the `Stripe-Account` header in order to take an application fee. For more information, see the application fees [documentation](https://stripe.com/docs/connect/direct-charges#collect-fees).
        """
        capture: NotRequired[bool]
        """
        Whether to immediately capture the charge. Defaults to `true`. When `false`, the charge issues an authorization (or pre-authorization), and will need to be [captured](https://stripe.com/docs/api#capture_charge) later. Uncaptured charges expire after a set number of days (7 by default). For more information, see the [authorizing charges and settling later](https://stripe.com/docs/charges/placing-a-hold) documentation.
        """
        currency: NotRequired[str]
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        customer: NotRequired[str]
        """
        The ID of an existing customer that will be charged in this request.
        """
        description: NotRequired[str]
        """
        An arbitrary string which you can attach to a `Charge` object. It is displayed when in the web interface alongside the charge. Note that if you use Stripe to send automatic email receipts to your customers, your receipt emails will include the `description` of the charge(s) that they are describing.
        """
        destination: NotRequired["ChargeService.CreateParamsDestination"]
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        on_behalf_of: NotRequired[str]
        """
        The Stripe account ID for which these funds are intended. Automatically set if you use the `destination` parameter. For details, see [Creating Separate Charges and Transfers](https://stripe.com/docs/connect/separate-charges-and-transfers#settlement-merchant).
        """
        radar_options: NotRequired["ChargeService.CreateParamsRadarOptions"]
        """
        Options to configure Radar. See [Radar Session](https://stripe.com/docs/radar/radar-session) for more information.
        """
        receipt_email: NotRequired[str]
        """
        The email address to which this charge's [receipt](https://stripe.com/docs/dashboard/receipts) will be sent. The receipt will not be sent until the charge is paid, and no receipts will be sent for test mode charges. If this charge is for a [Customer](https://stripe.com/docs/api/customers/object), the email address specified here will override the customer's email address. If `receipt_email` is specified for a charge in live mode, a receipt will be sent regardless of your [email settings](https://dashboard.stripe.com/account/emails).
        """
        shipping: NotRequired["ChargeService.CreateParamsShipping"]
        """
        Shipping information for the charge. Helps prevent fraud on charges for physical goods.
        """
        source: NotRequired[str]
        """
        A payment source to be charged. This can be the ID of a [card](https://stripe.com/docs/api#cards) (i.e., credit or debit card), a [bank account](https://stripe.com/docs/api#bank_accounts), a [source](https://stripe.com/docs/api#sources), a [token](https://stripe.com/docs/api#tokens), or a [connected account](https://stripe.com/docs/connect/account-debits#charging-a-connected-account). For certain sources---namely, [cards](https://stripe.com/docs/api#cards), [bank accounts](https://stripe.com/docs/api#bank_accounts), and attached [sources](https://stripe.com/docs/api#sources)---you must also pass the ID of the associated customer.
        """
        statement_descriptor: NotRequired[str]
        """
        For a non-card charge, text that appears on the customer's statement as the [statement descriptor](https://docs.stripe.com/get-started/account/statement-descriptors). This value overrides the account's default statement descriptor. For a card charge, this value is ignored unless you don't specify a `statement_descriptor_suffix`, in which case this value is used as the suffix.
        """
        statement_descriptor_suffix: NotRequired[str]
        """
        Provides information about a card charge. Concatenated to the account's [statement descriptor prefix](https://docs.corp.stripe.com/get-started/account/statement-descriptors#static) to form the complete statement descriptor that appears on the customer's statement. If the account has no prefix value, the suffix is concatenated to the account's statement descriptor.
        """
        transfer_data: NotRequired["ChargeService.CreateParamsTransferData"]
        """
        An optional dictionary including the account to automatically transfer to as part of a destination charge. [See the Connect documentation](https://stripe.com/docs/connect/destination-charges) for details.
        """
        transfer_group: NotRequired[str]
        """
        A string that identifies this transaction as part of a group. For details, see [Grouping transactions](https://stripe.com/docs/connect/separate-charges-and-transfers#transfer-options).
        """

    class CreateParamsDestination(TypedDict):
        account: str
        """
        ID of an existing, connected Stripe account.
        """
        amount: NotRequired[int]
        """
        The amount to transfer to the destination account without creating an `Application Fee` object. Cannot be combined with the `application_fee` parameter. Must be less than or equal to the charge amount.
        """

    class CreateParamsRadarOptions(TypedDict):
        session: NotRequired[str]
        """
        A [Radar Session](https://stripe.com/docs/radar/radar-session) is a snapshot of the browser metadata and device details that help Radar make more accurate predictions on your payments.
        """

    class CreateParamsShipping(TypedDict):
        address: "ChargeService.CreateParamsShippingAddress"
        """
        Shipping address.
        """
        carrier: NotRequired[str]
        """
        The delivery service that shipped a physical product, such as Fedex, UPS, USPS, etc.
        """
        name: str
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

    class CreateParamsShippingAddress(TypedDict):
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

    class CreateParamsTransferData(TypedDict):
        amount: NotRequired[int]
        """
        The amount transferred to the destination account, if specified. By default, the entire charge amount is transferred to the destination account.
        """
        destination: str
        """
        ID of an existing, connected Stripe account.
        """

    class ListParams(TypedDict):
        created: NotRequired["ChargeService.ListParamsCreated|int"]
        """
        Only return charges that were created during the given date interval.
        """
        customer: NotRequired[str]
        """
        Only return charges for the customer specified by this customer ID.
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
        Only return charges that were created by the PaymentIntent specified by this PaymentIntent ID.
        """
        starting_after: NotRequired[str]
        """
        A cursor for use in pagination. `starting_after` is an object ID that defines your place in the list. For instance, if you make a list request and receive 100 objects, ending with `obj_foo`, your subsequent call can include `starting_after=obj_foo` in order to fetch the next page of the list.
        """
        transfer_group: NotRequired[str]
        """
        Only return charges for this transfer group, limited to 100.
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

    class SearchParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        limit: NotRequired[int]
        """
        A limit on the number of objects to be returned. Limit can range between 1 and 100, and the default is 10.
        """
        page: NotRequired[str]
        """
        A cursor for pagination across multiple pages of results. Don't include this parameter on the first call. Use the next_page value returned in a previous response to request subsequent results.
        """
        query: str
        """
        The search query string. See [search query language](https://stripe.com/docs/search#search-query-language) and the list of supported [query fields for charges](https://stripe.com/docs/search#query-fields-for-charges).
        """

    class UpdateParams(TypedDict):
        customer: NotRequired[str]
        """
        The ID of an existing customer that will be associated with this request. This field may only be updated if there is no existing associated customer with this charge.
        """
        description: NotRequired[str]
        """
        An arbitrary string which you can attach to a charge object. It is displayed when in the web interface alongside the charge. Note that if you use Stripe to send automatic email receipts to your customers, your receipt emails will include the `description` of the charge(s) that they are describing.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        fraud_details: NotRequired["ChargeService.UpdateParamsFraudDetails"]
        """
        A set of key-value pairs you can attach to a charge giving information about its riskiness. If you believe a charge is fraudulent, include a `user_report` key with a value of `fraudulent`. If you believe a charge is safe, include a `user_report` key with a value of `safe`. Stripe will use the information you send to improve our fraud detection algorithms.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        receipt_email: NotRequired[str]
        """
        This is the email address that the receipt for this charge will be sent to. If this field is updated, then a new email receipt will be sent to the updated address.
        """
        shipping: NotRequired["ChargeService.UpdateParamsShipping"]
        """
        Shipping information for the charge. Helps prevent fraud on charges for physical goods.
        """
        transfer_group: NotRequired[str]
        """
        A string that identifies this transaction as part of a group. `transfer_group` may only be provided if it has not been set. See the [Connect documentation](https://stripe.com/docs/connect/separate-charges-and-transfers#transfer-options) for details.
        """

    class UpdateParamsFraudDetails(TypedDict):
        user_report: Union[Literal[""], Literal["fraudulent", "safe"]]
        """
        Either `safe` or `fraudulent`.
        """

    class UpdateParamsShipping(TypedDict):
        address: "ChargeService.UpdateParamsShippingAddress"
        """
        Shipping address.
        """
        carrier: NotRequired[str]
        """
        The delivery service that shipped a physical product, such as Fedex, UPS, USPS, etc.
        """
        name: str
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

    class UpdateParamsShippingAddress(TypedDict):
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

    def list(
        self,
        params: "ChargeService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Charge]:
        """
        Returns a list of charges you've previously created. The charges are returned in sorted order, with the most recent charges appearing first.
        """
        return cast(
            ListObject[Charge],
            self._request(
                "get",
                "/v1/charges",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "ChargeService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Charge]:
        """
        Returns a list of charges you've previously created. The charges are returned in sorted order, with the most recent charges appearing first.
        """
        return cast(
            ListObject[Charge],
            await self._request_async(
                "get",
                "/v1/charges",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self,
        params: "ChargeService.CreateParams" = {},
        options: RequestOptions = {},
    ) -> Charge:
        """
        This method is no longer recommended—use the [Payment Intents API](https://stripe.com/docs/api/payment_intents)
        to initiate a new payment instead. Confirmation of the PaymentIntent creates the Charge
        object used to request payment.
        """
        return cast(
            Charge,
            self._request(
                "post",
                "/v1/charges",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        params: "ChargeService.CreateParams" = {},
        options: RequestOptions = {},
    ) -> Charge:
        """
        This method is no longer recommended—use the [Payment Intents API](https://stripe.com/docs/api/payment_intents)
        to initiate a new payment instead. Confirmation of the PaymentIntent creates the Charge
        object used to request payment.
        """
        return cast(
            Charge,
            await self._request_async(
                "post",
                "/v1/charges",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        charge: str,
        params: "ChargeService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Charge:
        """
        Retrieves the details of a charge that has previously been created. Supply the unique charge ID that was returned from your previous request, and Stripe will return the corresponding charge information. The same information is returned when creating or refunding the charge.
        """
        return cast(
            Charge,
            self._request(
                "get",
                "/v1/charges/{charge}".format(charge=sanitize_id(charge)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        charge: str,
        params: "ChargeService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Charge:
        """
        Retrieves the details of a charge that has previously been created. Supply the unique charge ID that was returned from your previous request, and Stripe will return the corresponding charge information. The same information is returned when creating or refunding the charge.
        """
        return cast(
            Charge,
            await self._request_async(
                "get",
                "/v1/charges/{charge}".format(charge=sanitize_id(charge)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def update(
        self,
        charge: str,
        params: "ChargeService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Charge:
        """
        Updates the specified charge by setting the values of the parameters passed. Any parameters not provided will be left unchanged.
        """
        return cast(
            Charge,
            self._request(
                "post",
                "/v1/charges/{charge}".format(charge=sanitize_id(charge)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def update_async(
        self,
        charge: str,
        params: "ChargeService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Charge:
        """
        Updates the specified charge by setting the values of the parameters passed. Any parameters not provided will be left unchanged.
        """
        return cast(
            Charge,
            await self._request_async(
                "post",
                "/v1/charges/{charge}".format(charge=sanitize_id(charge)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def search(
        self,
        params: "ChargeService.SearchParams",
        options: RequestOptions = {},
    ) -> SearchResultObject[Charge]:
        """
        Search for charges you've previously created using Stripe's [Search Query Language](https://stripe.com/docs/search#search-query-language).
        Don't use search in read-after-write flows where strict consistency is necessary. Under normal operating
        conditions, data is searchable in less than a minute. Occasionally, propagation of new or updated data can be up
        to an hour behind during outages. Search functionality is not available to merchants in India.
        """
        return cast(
            SearchResultObject[Charge],
            self._request(
                "get",
                "/v1/charges/search",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def search_async(
        self,
        params: "ChargeService.SearchParams",
        options: RequestOptions = {},
    ) -> SearchResultObject[Charge]:
        """
        Search for charges you've previously created using Stripe's [Search Query Language](https://stripe.com/docs/search#search-query-language).
        Don't use search in read-after-write flows where strict consistency is necessary. Under normal operating
        conditions, data is searchable in less than a minute. Occasionally, propagation of new or updated data can be up
        to an hour behind during outages. Search functionality is not available to merchants in India.
        """
        return cast(
            SearchResultObject[Charge],
            await self._request_async(
                "get",
                "/v1/charges/search",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def capture(
        self,
        charge: str,
        params: "ChargeService.CaptureParams" = {},
        options: RequestOptions = {},
    ) -> Charge:
        """
        Capture the payment of an existing, uncaptured charge that was created with the capture option set to false.

        Uncaptured payments expire a set number of days after they are created ([7 by default](https://stripe.com/docs/charges/placing-a-hold)), after which they are marked as refunded and capture attempts will fail.

        Don't use this method to capture a PaymentIntent-initiated charge. Use [Capture a PaymentIntent](https://stripe.com/docs/api/payment_intents/capture).
        """
        return cast(
            Charge,
            self._request(
                "post",
                "/v1/charges/{charge}/capture".format(
                    charge=sanitize_id(charge),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def capture_async(
        self,
        charge: str,
        params: "ChargeService.CaptureParams" = {},
        options: RequestOptions = {},
    ) -> Charge:
        """
        Capture the payment of an existing, uncaptured charge that was created with the capture option set to false.

        Uncaptured payments expire a set number of days after they are created ([7 by default](https://stripe.com/docs/charges/placing-a-hold)), after which they are marked as refunded and capture attempts will fail.

        Don't use this method to capture a PaymentIntent-initiated charge. Use [Capture a PaymentIntent](https://stripe.com/docs/api/payment_intents/capture).
        """
        return cast(
            Charge,
            await self._request_async(
                "post",
                "/v1/charges/{charge}/capture".format(
                    charge=sanitize_id(charge),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._account import Account
from stripe._bank_account import BankAccount
from stripe._card import Card
from stripe._request_options import RequestOptions
from stripe._source import Source
from stripe._source_transaction_service import SourceTransactionService
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from typing import Dict, List, Union, cast
from typing_extensions import Literal, NotRequired, TypedDict


class SourceService(StripeService):
    def __init__(self, requestor):
        super().__init__(requestor)
        self.transactions = SourceTransactionService(self._requestor)

    class CreateParams(TypedDict):
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
        mandate: NotRequired["SourceService.CreateParamsMandate"]
        """
        Information about a mandate possibility attached to a source object (generally for bank debits) as well as its acceptance status.
        """
        metadata: NotRequired[Dict[str, str]]
        original_source: NotRequired[str]
        """
        The source to share.
        """
        owner: NotRequired["SourceService.CreateParamsOwner"]
        """
        Information about the owner of the payment instrument that may be used or required by particular source types.
        """
        receiver: NotRequired["SourceService.CreateParamsReceiver"]
        """
        Optional parameters for the receiver flow. Can be set only if the source is a receiver (`flow` is `receiver`).
        """
        redirect: NotRequired["SourceService.CreateParamsRedirect"]
        """
        Parameters required for the redirect flow. Required if the source is authenticated by a redirect (`flow` is `redirect`).
        """
        source_order: NotRequired["SourceService.CreateParamsSourceOrder"]
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
        acceptance: NotRequired["SourceService.CreateParamsMandateAcceptance"]
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
        offline: NotRequired[
            "SourceService.CreateParamsMandateAcceptanceOffline"
        ]
        """
        The parameters required to store a mandate accepted offline. Should only be set if `mandate[type]` is `offline`
        """
        online: NotRequired[
            "SourceService.CreateParamsMandateAcceptanceOnline"
        ]
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
        address: NotRequired["SourceService.CreateParamsOwnerAddress"]
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
        items: NotRequired[List["SourceService.CreateParamsSourceOrderItem"]]
        """
        List of items constituting the order.
        """
        shipping: NotRequired["SourceService.CreateParamsSourceOrderShipping"]
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
        address: "SourceService.CreateParamsSourceOrderShippingAddress"
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

    class DetachParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class RetrieveParams(TypedDict):
        client_secret: NotRequired[str]
        """
        The client secret of the source. Required if a publishable key is used to retrieve the source.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class UpdateParams(TypedDict):
        amount: NotRequired[int]
        """
        Amount associated with the source.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        mandate: NotRequired["SourceService.UpdateParamsMandate"]
        """
        Information about a mandate possibility attached to a source object (generally for bank debits) as well as its acceptance status.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        owner: NotRequired["SourceService.UpdateParamsOwner"]
        """
        Information about the owner of the payment instrument that may be used or required by particular source types.
        """
        source_order: NotRequired["SourceService.UpdateParamsSourceOrder"]
        """
        Information about the items and shipping associated with the source. Required for transactional credit (for example Klarna) sources before you can charge it.
        """

    class UpdateParamsMandate(TypedDict):
        acceptance: NotRequired["SourceService.UpdateParamsMandateAcceptance"]
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

    class UpdateParamsMandateAcceptance(TypedDict):
        date: NotRequired[int]
        """
        The Unix timestamp (in seconds) when the mandate was accepted or refused by the customer.
        """
        ip: NotRequired[str]
        """
        The IP address from which the mandate was accepted or refused by the customer.
        """
        offline: NotRequired[
            "SourceService.UpdateParamsMandateAcceptanceOffline"
        ]
        """
        The parameters required to store a mandate accepted offline. Should only be set if `mandate[type]` is `offline`
        """
        online: NotRequired[
            "SourceService.UpdateParamsMandateAcceptanceOnline"
        ]
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

    class UpdateParamsMandateAcceptanceOffline(TypedDict):
        contact_email: str
        """
        An email to contact you with if a copy of the mandate is requested, required if `type` is `offline`.
        """

    class UpdateParamsMandateAcceptanceOnline(TypedDict):
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

    class UpdateParamsOwner(TypedDict):
        address: NotRequired["SourceService.UpdateParamsOwnerAddress"]
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

    class UpdateParamsOwnerAddress(TypedDict):
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

    class UpdateParamsSourceOrder(TypedDict):
        items: NotRequired[List["SourceService.UpdateParamsSourceOrderItem"]]
        """
        List of items constituting the order.
        """
        shipping: NotRequired["SourceService.UpdateParamsSourceOrderShipping"]
        """
        Shipping address for the order. Required if any of the SKUs are for products that have `shippable` set to true.
        """

    class UpdateParamsSourceOrderItem(TypedDict):
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

    class UpdateParamsSourceOrderShipping(TypedDict):
        address: "SourceService.UpdateParamsSourceOrderShippingAddress"
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

    class UpdateParamsSourceOrderShippingAddress(TypedDict):
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

    class VerifyParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        values: List[str]
        """
        The values needed to verify the source.
        """

    def detach(
        self,
        customer: str,
        id: str,
        params: "SourceService.DetachParams" = {},
        options: RequestOptions = {},
    ) -> Union[Account, BankAccount, Card, Source]:
        """
        Delete a specified source for a given customer.
        """
        return cast(
            Union[Account, BankAccount, Card, Source],
            self._request(
                "delete",
                "/v1/customers/{customer}/sources/{id}".format(
                    customer=sanitize_id(customer),
                    id=sanitize_id(id),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def detach_async(
        self,
        customer: str,
        id: str,
        params: "SourceService.DetachParams" = {},
        options: RequestOptions = {},
    ) -> Union[Account, BankAccount, Card, Source]:
        """
        Delete a specified source for a given customer.
        """
        return cast(
            Union[Account, BankAccount, Card, Source],
            await self._request_async(
                "delete",
                "/v1/customers/{customer}/sources/{id}".format(
                    customer=sanitize_id(customer),
                    id=sanitize_id(id),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        source: str,
        params: "SourceService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Source:
        """
        Retrieves an existing source object. Supply the unique source ID from a source creation request and Stripe will return the corresponding up-to-date source object information.
        """
        return cast(
            Source,
            self._request(
                "get",
                "/v1/sources/{source}".format(source=sanitize_id(source)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        source: str,
        params: "SourceService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Source:
        """
        Retrieves an existing source object. Supply the unique source ID from a source creation request and Stripe will return the corresponding up-to-date source object information.
        """
        return cast(
            Source,
            await self._request_async(
                "get",
                "/v1/sources/{source}".format(source=sanitize_id(source)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def update(
        self,
        source: str,
        params: "SourceService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Source:
        """
        Updates the specified source by setting the values of the parameters passed. Any parameters not provided will be left unchanged.

        This request accepts the metadata and owner as arguments. It is also possible to update type specific information for selected payment methods. Please refer to our [payment method guides](https://stripe.com/docs/sources) for more detail.
        """
        return cast(
            Source,
            self._request(
                "post",
                "/v1/sources/{source}".format(source=sanitize_id(source)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def update_async(
        self,
        source: str,
        params: "SourceService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Source:
        """
        Updates the specified source by setting the values of the parameters passed. Any parameters not provided will be left unchanged.

        This request accepts the metadata and owner as arguments. It is also possible to update type specific information for selected payment methods. Please refer to our [payment method guides](https://stripe.com/docs/sources) for more detail.
        """
        return cast(
            Source,
            await self._request_async(
                "post",
                "/v1/sources/{source}".format(source=sanitize_id(source)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self,
        params: "SourceService.CreateParams" = {},
        options: RequestOptions = {},
    ) -> Source:
        """
        Creates a new source object.
        """
        return cast(
            Source,
            self._request(
                "post",
                "/v1/sources",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        params: "SourceService.CreateParams" = {},
        options: RequestOptions = {},
    ) -> Source:
        """
        Creates a new source object.
        """
        return cast(
            Source,
            await self._request_async(
                "post",
                "/v1/sources",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def verify(
        self,
        source: str,
        params: "SourceService.VerifyParams",
        options: RequestOptions = {},
    ) -> Source:
        """
        Verify a given source.
        """
        return cast(
            Source,
            self._request(
                "post",
                "/v1/sources/{source}/verify".format(
                    source=sanitize_id(source),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def verify_async(
        self,
        source: str,
        params: "SourceService.VerifyParams",
        options: RequestOptions = {},
    ) -> Source:
        """
        Verify a given source.
        """
        return cast(
            Source,
            await self._request_async(
                "post",
                "/v1/sources/{source}/verify".format(
                    source=sanitize_id(source),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._customer import Customer
from stripe._customer_balance_transaction_service import (
    CustomerBalanceTransactionService,
)
from stripe._customer_cash_balance_service import CustomerCashBalanceService
from stripe._customer_cash_balance_transaction_service import (
    CustomerCashBalanceTransactionService,
)
from stripe._customer_funding_instructions_service import (
    CustomerFundingInstructionsService,
)
from stripe._customer_payment_method_service import (
    CustomerPaymentMethodService,
)
from stripe._customer_payment_source_service import (
    CustomerPaymentSourceService,
)
from stripe._customer_tax_id_service import CustomerTaxIdService
from stripe._discount import Discount
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._search_result_object import SearchResultObject
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from typing import Dict, List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class CustomerService(StripeService):
    def __init__(self, requestor):
        super().__init__(requestor)
        self.cash_balance = CustomerCashBalanceService(self._requestor)
        self.balance_transactions = CustomerBalanceTransactionService(
            self._requestor,
        )
        self.cash_balance_transactions = CustomerCashBalanceTransactionService(
            self._requestor,
        )
        self.payment_sources = CustomerPaymentSourceService(self._requestor)
        self.tax_ids = CustomerTaxIdService(self._requestor)
        self.payment_methods = CustomerPaymentMethodService(self._requestor)
        self.funding_instructions = CustomerFundingInstructionsService(
            self._requestor,
        )

    class CreateParams(TypedDict):
        address: NotRequired["Literal['']|CustomerService.CreateParamsAddress"]
        """
        The customer's address.
        """
        balance: NotRequired[int]
        """
        An integer amount in cents (or local equivalent) that represents the customer's current balance, which affect the customer's future invoices. A negative amount represents a credit that decreases the amount due on an invoice; a positive amount increases the amount due on an invoice.
        """
        cash_balance: NotRequired["CustomerService.CreateParamsCashBalance"]
        """
        Balance information and default balance settings for this customer.
        """
        coupon: NotRequired[str]
        description: NotRequired[str]
        """
        An arbitrary string that you can attach to a customer object. It is displayed alongside the customer in the dashboard.
        """
        email: NotRequired[str]
        """
        Customer's email address. It's displayed alongside the customer in your dashboard and can be useful for searching and tracking. This may be up to *512 characters*.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        invoice_prefix: NotRequired[str]
        """
        The prefix for the customer used to generate unique invoice numbers. Must be 3–12 uppercase letters or numbers.
        """
        invoice_settings: NotRequired[
            "CustomerService.CreateParamsInvoiceSettings"
        ]
        """
        Default invoice settings for this customer.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        name: NotRequired[str]
        """
        The customer's full name or business name.
        """
        next_invoice_sequence: NotRequired[int]
        """
        The sequence to be used on the customer's next invoice. Defaults to 1.
        """
        payment_method: NotRequired[str]
        phone: NotRequired[str]
        """
        The customer's phone number.
        """
        preferred_locales: NotRequired[List[str]]
        """
        Customer's preferred languages, ordered by preference.
        """
        promotion_code: NotRequired[str]
        """
        The ID of a promotion code to apply to the customer. The customer will have a discount applied on all recurring payments. Charges you create through the API will not have the discount.
        """
        shipping: NotRequired[
            "Literal['']|CustomerService.CreateParamsShipping"
        ]
        """
        The customer's shipping information. Appears on invoices emailed to this customer.
        """
        source: NotRequired[str]
        tax: NotRequired["CustomerService.CreateParamsTax"]
        """
        Tax details about the customer.
        """
        tax_exempt: NotRequired[
            "Literal['']|Literal['exempt', 'none', 'reverse']"
        ]
        """
        The customer's tax exemption. One of `none`, `exempt`, or `reverse`.
        """
        tax_id_data: NotRequired[
            List["CustomerService.CreateParamsTaxIdDatum"]
        ]
        """
        The customer's tax IDs.
        """
        test_clock: NotRequired[str]
        """
        ID of the test clock to attach to the customer.
        """
        validate: NotRequired[bool]

    class CreateParamsAddress(TypedDict):
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

    class CreateParamsCashBalance(TypedDict):
        settings: NotRequired[
            "CustomerService.CreateParamsCashBalanceSettings"
        ]
        """
        Settings controlling the behavior of the customer's cash balance,
        such as reconciliation of funds received.
        """

    class CreateParamsCashBalanceSettings(TypedDict):
        reconciliation_mode: NotRequired[
            Literal["automatic", "manual", "merchant_default"]
        ]
        """
        Controls how funds transferred by the customer are applied to payment intents and invoices. Valid options are `automatic`, `manual`, or `merchant_default`. For more information about these reconciliation modes, see [Reconciliation](https://stripe.com/docs/payments/customer-balance/reconciliation).
        """

    class CreateParamsInvoiceSettings(TypedDict):
        custom_fields: NotRequired[
            "Literal['']|List[CustomerService.CreateParamsInvoiceSettingsCustomField]"
        ]
        """
        The list of up to 4 default custom fields to be displayed on invoices for this customer. When updating, pass an empty string to remove previously-defined fields.
        """
        default_payment_method: NotRequired[str]
        """
        ID of a payment method that's attached to the customer, to be used as the customer's default payment method for subscriptions and invoices.
        """
        footer: NotRequired[str]
        """
        Default footer to be displayed on invoices for this customer.
        """
        rendering_options: NotRequired[
            "Literal['']|CustomerService.CreateParamsInvoiceSettingsRenderingOptions"
        ]
        """
        Default options for invoice PDF rendering for this customer.
        """

    class CreateParamsInvoiceSettingsCustomField(TypedDict):
        name: str
        """
        The name of the custom field. This may be up to 40 characters.
        """
        value: str
        """
        The value of the custom field. This may be up to 140 characters.
        """

    class CreateParamsInvoiceSettingsRenderingOptions(TypedDict):
        amount_tax_display: NotRequired[
            "Literal['']|Literal['exclude_tax', 'include_inclusive_tax']"
        ]
        """
        How line-item prices and amounts will be displayed with respect to tax on invoice PDFs. One of `exclude_tax` or `include_inclusive_tax`. `include_inclusive_tax` will include inclusive tax (and exclude exclusive tax) in invoice PDF amounts. `exclude_tax` will exclude all tax (inclusive and exclusive alike) from invoice PDF amounts.
        """

    class CreateParamsShipping(TypedDict):
        address: "CustomerService.CreateParamsShippingAddress"
        """
        Customer shipping address.
        """
        name: str
        """
        Customer name.
        """
        phone: NotRequired[str]
        """
        Customer phone (including extension).
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

    class CreateParamsTax(TypedDict):
        ip_address: NotRequired["Literal['']|str"]
        """
        A recent IP address of the customer used for tax reporting and tax location inference. Stripe recommends updating the IP address when a new PaymentMethod is attached or the address field on the customer is updated. We recommend against updating this field more frequently since it could result in unexpected tax location/reporting outcomes.
        """
        validate_location: NotRequired[Literal["deferred", "immediately"]]
        """
        A flag that indicates when Stripe should validate the customer tax location. Defaults to `deferred`.
        """

    class CreateParamsTaxIdDatum(TypedDict):
        type: Literal[
            "ad_nrt",
            "ae_trn",
            "ar_cuit",
            "au_abn",
            "au_arn",
            "bg_uic",
            "bh_vat",
            "bo_tin",
            "br_cnpj",
            "br_cpf",
            "ca_bn",
            "ca_gst_hst",
            "ca_pst_bc",
            "ca_pst_mb",
            "ca_pst_sk",
            "ca_qst",
            "ch_uid",
            "ch_vat",
            "cl_tin",
            "cn_tin",
            "co_nit",
            "cr_tin",
            "de_stn",
            "do_rcn",
            "ec_ruc",
            "eg_tin",
            "es_cif",
            "eu_oss_vat",
            "eu_vat",
            "gb_vat",
            "ge_vat",
            "hk_br",
            "hu_tin",
            "id_npwp",
            "il_vat",
            "in_gst",
            "is_vat",
            "jp_cn",
            "jp_rn",
            "jp_trn",
            "ke_pin",
            "kr_brn",
            "kz_bin",
            "li_uid",
            "mx_rfc",
            "my_frp",
            "my_itn",
            "my_sst",
            "ng_tin",
            "no_vat",
            "no_voec",
            "nz_gst",
            "om_vat",
            "pe_ruc",
            "ph_tin",
            "ro_tin",
            "rs_pib",
            "ru_inn",
            "ru_kpp",
            "sa_vat",
            "sg_gst",
            "sg_uen",
            "si_tin",
            "sv_nit",
            "th_vat",
            "tr_tin",
            "tw_vat",
            "ua_vat",
            "us_ein",
            "uy_ruc",
            "ve_rif",
            "vn_tin",
            "za_vat",
        ]
        """
        Type of the tax ID, one of `ad_nrt`, `ae_trn`, `ar_cuit`, `au_abn`, `au_arn`, `bg_uic`, `bh_vat`, `bo_tin`, `br_cnpj`, `br_cpf`, `ca_bn`, `ca_gst_hst`, `ca_pst_bc`, `ca_pst_mb`, `ca_pst_sk`, `ca_qst`, `ch_uid`, `ch_vat`, `cl_tin`, `cn_tin`, `co_nit`, `cr_tin`, `de_stn`, `do_rcn`, `ec_ruc`, `eg_tin`, `es_cif`, `eu_oss_vat`, `eu_vat`, `gb_vat`, `ge_vat`, `hk_br`, `hu_tin`, `id_npwp`, `il_vat`, `in_gst`, `is_vat`, `jp_cn`, `jp_rn`, `jp_trn`, `ke_pin`, `kr_brn`, `kz_bin`, `li_uid`, `mx_rfc`, `my_frp`, `my_itn`, `my_sst`, `ng_tin`, `no_vat`, `no_voec`, `nz_gst`, `om_vat`, `pe_ruc`, `ph_tin`, `ro_tin`, `rs_pib`, `ru_inn`, `ru_kpp`, `sa_vat`, `sg_gst`, `sg_uen`, `si_tin`, `sv_nit`, `th_vat`, `tr_tin`, `tw_vat`, `ua_vat`, `us_ein`, `uy_ruc`, `ve_rif`, `vn_tin`, or `za_vat`
        """
        value: str
        """
        Value of the tax ID.
        """

    class DeleteDiscountParams(TypedDict):
        pass

    class DeleteParams(TypedDict):
        pass

    class ListParams(TypedDict):
        created: NotRequired["CustomerService.ListParamsCreated|int"]
        """
        Only return customers that were created during the given date interval.
        """
        email: NotRequired[str]
        """
        A case-sensitive filter on the list based on the customer's `email` field. The value must be a string.
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
        test_clock: NotRequired[str]
        """
        Provides a list of customers that are associated with the specified test clock. The response will not include customers with test clocks if this parameter is not set.
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
        The search query string. See [search query language](https://stripe.com/docs/search#search-query-language) and the list of supported [query fields for customers](https://stripe.com/docs/search#query-fields-for-customers).
        """

    class UpdateParams(TypedDict):
        address: NotRequired["Literal['']|CustomerService.UpdateParamsAddress"]
        """
        The customer's address.
        """
        balance: NotRequired[int]
        """
        An integer amount in cents (or local equivalent) that represents the customer's current balance, which affect the customer's future invoices. A negative amount represents a credit that decreases the amount due on an invoice; a positive amount increases the amount due on an invoice.
        """
        cash_balance: NotRequired["CustomerService.UpdateParamsCashBalance"]
        """
        Balance information and default balance settings for this customer.
        """
        coupon: NotRequired[str]
        default_source: NotRequired[str]
        """
        If you are using payment methods created via the PaymentMethods API, see the [invoice_settings.default_payment_method](https://stripe.com/docs/api/customers/update#update_customer-invoice_settings-default_payment_method) parameter.

        Provide the ID of a payment source already attached to this customer to make it this customer's default payment source.

        If you want to add a new payment source and make it the default, see the [source](https://stripe.com/docs/api/customers/update#update_customer-source) property.
        """
        description: NotRequired[str]
        """
        An arbitrary string that you can attach to a customer object. It is displayed alongside the customer in the dashboard.
        """
        email: NotRequired[str]
        """
        Customer's email address. It's displayed alongside the customer in your dashboard and can be useful for searching and tracking. This may be up to *512 characters*.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        invoice_prefix: NotRequired[str]
        """
        The prefix for the customer used to generate unique invoice numbers. Must be 3–12 uppercase letters or numbers.
        """
        invoice_settings: NotRequired[
            "CustomerService.UpdateParamsInvoiceSettings"
        ]
        """
        Default invoice settings for this customer.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        name: NotRequired[str]
        """
        The customer's full name or business name.
        """
        next_invoice_sequence: NotRequired[int]
        """
        The sequence to be used on the customer's next invoice. Defaults to 1.
        """
        phone: NotRequired[str]
        """
        The customer's phone number.
        """
        preferred_locales: NotRequired[List[str]]
        """
        Customer's preferred languages, ordered by preference.
        """
        promotion_code: NotRequired[str]
        """
        The ID of a promotion code to apply to the customer. The customer will have a discount applied on all recurring payments. Charges you create through the API will not have the discount.
        """
        shipping: NotRequired[
            "Literal['']|CustomerService.UpdateParamsShipping"
        ]
        """
        The customer's shipping information. Appears on invoices emailed to this customer.
        """
        source: NotRequired[str]
        tax: NotRequired["CustomerService.UpdateParamsTax"]
        """
        Tax details about the customer.
        """
        tax_exempt: NotRequired[
            "Literal['']|Literal['exempt', 'none', 'reverse']"
        ]
        """
        The customer's tax exemption. One of `none`, `exempt`, or `reverse`.
        """
        validate: NotRequired[bool]

    class UpdateParamsAddress(TypedDict):
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

    class UpdateParamsCashBalance(TypedDict):
        settings: NotRequired[
            "CustomerService.UpdateParamsCashBalanceSettings"
        ]
        """
        Settings controlling the behavior of the customer's cash balance,
        such as reconciliation of funds received.
        """

    class UpdateParamsCashBalanceSettings(TypedDict):
        reconciliation_mode: NotRequired[
            Literal["automatic", "manual", "merchant_default"]
        ]
        """
        Controls how funds transferred by the customer are applied to payment intents and invoices. Valid options are `automatic`, `manual`, or `merchant_default`. For more information about these reconciliation modes, see [Reconciliation](https://stripe.com/docs/payments/customer-balance/reconciliation).
        """

    class UpdateParamsInvoiceSettings(TypedDict):
        custom_fields: NotRequired[
            "Literal['']|List[CustomerService.UpdateParamsInvoiceSettingsCustomField]"
        ]
        """
        The list of up to 4 default custom fields to be displayed on invoices for this customer. When updating, pass an empty string to remove previously-defined fields.
        """
        default_payment_method: NotRequired[str]
        """
        ID of a payment method that's attached to the customer, to be used as the customer's default payment method for subscriptions and invoices.
        """
        footer: NotRequired[str]
        """
        Default footer to be displayed on invoices for this customer.
        """
        rendering_options: NotRequired[
            "Literal['']|CustomerService.UpdateParamsInvoiceSettingsRenderingOptions"
        ]
        """
        Default options for invoice PDF rendering for this customer.
        """

    class UpdateParamsInvoiceSettingsCustomField(TypedDict):
        name: str
        """
        The name of the custom field. This may be up to 40 characters.
        """
        value: str
        """
        The value of the custom field. This may be up to 140 characters.
        """

    class UpdateParamsInvoiceSettingsRenderingOptions(TypedDict):
        amount_tax_display: NotRequired[
            "Literal['']|Literal['exclude_tax', 'include_inclusive_tax']"
        ]
        """
        How line-item prices and amounts will be displayed with respect to tax on invoice PDFs. One of `exclude_tax` or `include_inclusive_tax`. `include_inclusive_tax` will include inclusive tax (and exclude exclusive tax) in invoice PDF amounts. `exclude_tax` will exclude all tax (inclusive and exclusive alike) from invoice PDF amounts.
        """

    class UpdateParamsShipping(TypedDict):
        address: "CustomerService.UpdateParamsShippingAddress"
        """
        Customer shipping address.
        """
        name: str
        """
        Customer name.
        """
        phone: NotRequired[str]
        """
        Customer phone (including extension).
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

    class UpdateParamsTax(TypedDict):
        ip_address: NotRequired["Literal['']|str"]
        """
        A recent IP address of the customer used for tax reporting and tax location inference. Stripe recommends updating the IP address when a new PaymentMethod is attached or the address field on the customer is updated. We recommend against updating this field more frequently since it could result in unexpected tax location/reporting outcomes.
        """
        validate_location: NotRequired[Literal["deferred", "immediately"]]
        """
        A flag that indicates when Stripe should validate the customer tax location. Defaults to `deferred`.
        """

    def delete(
        self,
        customer: str,
        params: "CustomerService.DeleteParams" = {},
        options: RequestOptions = {},
    ) -> Customer:
        """
        Permanently deletes a customer. It cannot be undone. Also immediately cancels any active subscriptions on the customer.
        """
        return cast(
            Customer,
            self._request(
                "delete",
                "/v1/customers/{customer}".format(
                    customer=sanitize_id(customer),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def delete_async(
        self,
        customer: str,
        params: "CustomerService.DeleteParams" = {},
        options: RequestOptions = {},
    ) -> Customer:
        """
        Permanently deletes a customer. It cannot be undone. Also immediately cancels any active subscriptions on the customer.
        """
        return cast(
            Customer,
            await self._request_async(
                "delete",
                "/v1/customers/{customer}".format(
                    customer=sanitize_id(customer),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        customer: str,
        params: "CustomerService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Customer:
        """
        Retrieves a Customer object.
        """
        return cast(
            Customer,
            self._request(
                "get",
                "/v1/customers/{customer}".format(
                    customer=sanitize_id(customer),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        customer: str,
        params: "CustomerService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Customer:
        """
        Retrieves a Customer object.
        """
        return cast(
            Customer,
            await self._request_async(
                "get",
                "/v1/customers/{customer}".format(
                    customer=sanitize_id(customer),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def update(
        self,
        customer: str,
        params: "CustomerService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Customer:
        """
        Updates the specified customer by setting the values of the parameters passed. Any parameters not provided will be left unchanged. For example, if you pass the source parameter, that becomes the customer's active source (e.g., a card) to be used for all charges in the future. When you update a customer to a new valid card source by passing the source parameter: for each of the customer's current subscriptions, if the subscription bills automatically and is in the past_due state, then the latest open invoice for the subscription with automatic collection enabled will be retried. This retry will not count as an automatic retry, and will not affect the next regularly scheduled payment for the invoice. Changing the default_source for a customer will not trigger this behavior.

        This request accepts mostly the same arguments as the customer creation call.
        """
        return cast(
            Customer,
            self._request(
                "post",
                "/v1/customers/{customer}".format(
                    customer=sanitize_id(customer),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def update_async(
        self,
        customer: str,
        params: "CustomerService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Customer:
        """
        Updates the specified customer by setting the values of the parameters passed. Any parameters not provided will be left unchanged. For example, if you pass the source parameter, that becomes the customer's active source (e.g., a card) to be used for all charges in the future. When you update a customer to a new valid card source by passing the source parameter: for each of the customer's current subscriptions, if the subscription bills automatically and is in the past_due state, then the latest open invoice for the subscription with automatic collection enabled will be retried. This retry will not count as an automatic retry, and will not affect the next regularly scheduled payment for the invoice. Changing the default_source for a customer will not trigger this behavior.

        This request accepts mostly the same arguments as the customer creation call.
        """
        return cast(
            Customer,
            await self._request_async(
                "post",
                "/v1/customers/{customer}".format(
                    customer=sanitize_id(customer),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def delete_discount(
        self,
        customer: str,
        params: "CustomerService.DeleteDiscountParams" = {},
        options: RequestOptions = {},
    ) -> Discount:
        """
        Removes the currently applied discount on a customer.
        """
        return cast(
            Discount,
            self._request(
                "delete",
                "/v1/customers/{customer}/discount".format(
                    customer=sanitize_id(customer),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def delete_discount_async(
        self,
        customer: str,
        params: "CustomerService.DeleteDiscountParams" = {},
        options: RequestOptions = {},
    ) -> Discount:
        """
        Removes the currently applied discount on a customer.
        """
        return cast(
            Discount,
            await self._request_async(
                "delete",
                "/v1/customers/{customer}/discount".format(
                    customer=sanitize_id(customer),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def list(
        self,
        params: "CustomerService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Customer]:
        """
        Returns a list of your customers. The customers are returned sorted by creation date, with the most recent customers appearing first.
        """
        return cast(
            ListObject[Customer],
            self._request(
                "get",
                "/v1/customers",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "CustomerService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Customer]:
        """
        Returns a list of your customers. The customers are returned sorted by creation date, with the most recent customers appearing first.
        """
        return cast(
            ListObject[Customer],
            await self._request_async(
                "get",
                "/v1/customers",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self,
        params: "CustomerService.CreateParams" = {},
        options: RequestOptions = {},
    ) -> Customer:
        """
        Creates a new customer object.
        """
        return cast(
            Customer,
            self._request(
                "post",
                "/v1/customers",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        params: "CustomerService.CreateParams" = {},
        options: RequestOptions = {},
    ) -> Customer:
        """
        Creates a new customer object.
        """
        return cast(
            Customer,
            await self._request_async(
                "post",
                "/v1/customers",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def search(
        self,
        params: "CustomerService.SearchParams",
        options: RequestOptions = {},
    ) -> SearchResultObject[Customer]:
        """
        Search for customers you've previously created using Stripe's [Search Query Language](https://stripe.com/docs/search#search-query-language).
        Don't use search in read-after-write flows where strict consistency is necessary. Under normal operating
        conditions, data is searchable in less than a minute. Occasionally, propagation of new or updated data can be up
        to an hour behind during outages. Search functionality is not available to merchants in India.
        """
        return cast(
            SearchResultObject[Customer],
            self._request(
                "get",
                "/v1/customers/search",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def search_async(
        self,
        params: "CustomerService.SearchParams",
        options: RequestOptions = {},
    ) -> SearchResultObject[Customer]:
        """
        Search for customers you've previously created using Stripe's [Search Query Language](https://stripe.com/docs/search#search-query-language).
        Don't use search in read-after-write flows where strict consistency is necessary. Under normal operating
        conditions, data is searchable in less than a minute. Occasionally, propagation of new or updated data can be up
        to an hour behind during outages. Search functionality is not available to merchants in India.
        """
        return cast(
            SearchResultObject[Customer],
            await self._request_async(
                "get",
                "/v1/customers/search",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

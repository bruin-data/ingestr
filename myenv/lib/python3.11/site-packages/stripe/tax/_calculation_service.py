# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from stripe.tax._calculation import Calculation
from stripe.tax._calculation_line_item_service import (
    CalculationLineItemService,
)
from typing import List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class CalculationService(StripeService):
    def __init__(self, requestor):
        super().__init__(requestor)
        self.line_items = CalculationLineItemService(self._requestor)

    class CreateParams(TypedDict):
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        customer: NotRequired[str]
        """
        The ID of an existing customer to use for this calculation. If provided, the customer's address and tax IDs are copied to `customer_details`.
        """
        customer_details: NotRequired[
            "CalculationService.CreateParamsCustomerDetails"
        ]
        """
        Details about the customer, including address and tax IDs.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        line_items: List["CalculationService.CreateParamsLineItem"]
        """
        A list of items the customer is purchasing.
        """
        ship_from_details: NotRequired[
            "CalculationService.CreateParamsShipFromDetails"
        ]
        """
        Details about the address from which the goods are being shipped.
        """
        shipping_cost: NotRequired[
            "CalculationService.CreateParamsShippingCost"
        ]
        """
        Shipping cost details to be used for the calculation.
        """
        tax_date: NotRequired[int]
        """
        Timestamp of date at which the tax rules and rates in effect applies for the calculation. Measured in seconds since the Unix epoch. Can be up to 48 hours in the past, and up to 48 hours in the future.
        """

    class CreateParamsCustomerDetails(TypedDict):
        address: NotRequired[
            "CalculationService.CreateParamsCustomerDetailsAddress"
        ]
        """
        The customer's postal address (for example, home or business location).
        """
        address_source: NotRequired[Literal["billing", "shipping"]]
        """
        The type of customer address provided.
        """
        ip_address: NotRequired[str]
        """
        The customer's IP address (IPv4 or IPv6).
        """
        tax_ids: NotRequired[
            List["CalculationService.CreateParamsCustomerDetailsTaxId"]
        ]
        """
        The customer's tax IDs. Stripe Tax might consider a transaction with applicable tax IDs to be B2B, which might affect the tax calculation result. Stripe Tax doesn't validate tax IDs for correctness.
        """
        taxability_override: NotRequired[
            Literal["customer_exempt", "none", "reverse_charge"]
        ]
        """
        Overrides the tax calculation result to allow you to not collect tax from your customer. Use this if you've manually checked your customer's tax exemptions. Prefer providing the customer's `tax_ids` where possible, which automatically determines whether `reverse_charge` applies.
        """

    class CreateParamsCustomerDetailsAddress(TypedDict):
        city: NotRequired["Literal['']|str"]
        """
        City, district, suburb, town, or village.
        """
        country: str
        """
        Two-letter country code ([ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)).
        """
        line1: NotRequired["Literal['']|str"]
        """
        Address line 1 (e.g., street, PO Box, or company name).
        """
        line2: NotRequired["Literal['']|str"]
        """
        Address line 2 (e.g., apartment, suite, unit, or building).
        """
        postal_code: NotRequired["Literal['']|str"]
        """
        ZIP or postal code.
        """
        state: NotRequired["Literal['']|str"]
        """
        State, county, province, or region. We recommend sending [ISO 3166-2](https://en.wikipedia.org/wiki/ISO_3166-2) subdivision code value when possible.
        """

    class CreateParamsCustomerDetailsTaxId(TypedDict):
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

    class CreateParamsLineItem(TypedDict):
        amount: int
        """
        A positive integer representing the line item's total price in the [smallest currency unit](https://stripe.com/docs/currencies#zero-decimal).
        If `tax_behavior=inclusive`, then this amount includes taxes. Otherwise, taxes are calculated on top of this amount.
        """
        product: NotRequired[str]
        """
        If provided, the product's `tax_code` will be used as the line item's `tax_code`.
        """
        quantity: NotRequired[int]
        """
        The number of units of the item being purchased. Used to calculate the per-unit price from the total `amount` for the line. For example, if `amount=100` and `quantity=4`, the calculated unit price is 25.
        """
        reference: NotRequired[str]
        """
        A custom identifier for this line item, which must be unique across the line items in the calculation. The reference helps identify each line item in exported [tax reports](https://stripe.com/docs/tax/reports).
        """
        tax_behavior: NotRequired[Literal["exclusive", "inclusive"]]
        """
        Specifies whether the `amount` includes taxes. Defaults to `exclusive`.
        """
        tax_code: NotRequired[str]
        """
        A [tax code](https://stripe.com/docs/tax/tax-categories) ID to use for this line item. If not provided, we will use the tax code from the provided `product` param. If neither `tax_code` nor `product` is provided, we will use the default tax code from your Tax Settings.
        """

    class CreateParamsShipFromDetails(TypedDict):
        address: "CalculationService.CreateParamsShipFromDetailsAddress"
        """
        The address from which the goods are being shipped from.
        """

    class CreateParamsShipFromDetailsAddress(TypedDict):
        city: NotRequired["Literal['']|str"]
        """
        City, district, suburb, town, or village.
        """
        country: str
        """
        Two-letter country code ([ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)).
        """
        line1: NotRequired["Literal['']|str"]
        """
        Address line 1 (e.g., street, PO Box, or company name).
        """
        line2: NotRequired["Literal['']|str"]
        """
        Address line 2 (e.g., apartment, suite, unit, or building).
        """
        postal_code: NotRequired["Literal['']|str"]
        """
        ZIP or postal code.
        """
        state: NotRequired["Literal['']|str"]
        """
        State/province as an [ISO 3166-2](https://en.wikipedia.org/wiki/ISO_3166-2) subdivision code, without country prefix. Example: "NY" or "TX".
        """

    class CreateParamsShippingCost(TypedDict):
        amount: NotRequired[int]
        """
        A positive integer in the [smallest currency unit](https://stripe.com/docs/currencies#zero-decimal) representing the shipping charge. If `tax_behavior=inclusive`, then this amount includes taxes. Otherwise, taxes are calculated on top of this amount.
        """
        shipping_rate: NotRequired[str]
        """
        If provided, the [shipping rate](https://stripe.com/docs/api/shipping_rates/object)'s `amount`, `tax_code` and `tax_behavior` are used. If you provide a shipping rate, then you cannot pass the `amount`, `tax_code`, or `tax_behavior` parameters.
        """
        tax_behavior: NotRequired[Literal["exclusive", "inclusive"]]
        """
        Specifies whether the `amount` includes taxes. If `tax_behavior=inclusive`, then the amount includes taxes. Defaults to `exclusive`.
        """
        tax_code: NotRequired[str]
        """
        The [tax code](https://stripe.com/docs/tax/tax-categories) used to calculate tax on shipping. If not provided, the default shipping tax code from your [Tax Settings](https://stripe.com/settings/tax) is used.
        """

    class RetrieveParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    def retrieve(
        self,
        calculation: str,
        params: "CalculationService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Calculation:
        """
        Retrieves a Tax Calculation object, if the calculation hasn't expired.
        """
        return cast(
            Calculation,
            self._request(
                "get",
                "/v1/tax/calculations/{calculation}".format(
                    calculation=sanitize_id(calculation),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        calculation: str,
        params: "CalculationService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Calculation:
        """
        Retrieves a Tax Calculation object, if the calculation hasn't expired.
        """
        return cast(
            Calculation,
            await self._request_async(
                "get",
                "/v1/tax/calculations/{calculation}".format(
                    calculation=sanitize_id(calculation),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self,
        params: "CalculationService.CreateParams",
        options: RequestOptions = {},
    ) -> Calculation:
        """
        Calculates tax based on the input and returns a Tax Calculation object.
        """
        return cast(
            Calculation,
            self._request(
                "post",
                "/v1/tax/calculations",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        params: "CalculationService.CreateParams",
        options: RequestOptions = {},
    ) -> Calculation:
        """
        Calculates tax based on the input and returns a Tax Calculation object.
        """
        return cast(
            Calculation,
            await self._request_async(
                "post",
                "/v1/tax/calculations",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

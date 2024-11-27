# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._createable_api_resource import CreateableAPIResource
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_object import StripeObject
from stripe._util import class_method_variant, sanitize_id
from typing import ClassVar, List, Optional, cast, overload
from typing_extensions import (
    Literal,
    NotRequired,
    TypedDict,
    Unpack,
    TYPE_CHECKING,
)

if TYPE_CHECKING:
    from stripe.tax._calculation_line_item import CalculationLineItem


class Calculation(CreateableAPIResource["Calculation"]):
    """
    A Tax Calculation allows you to calculate the tax to collect from your customer.

    Related guide: [Calculate tax in your custom payment flow](https://stripe.com/docs/tax/custom)
    """

    OBJECT_NAME: ClassVar[Literal["tax.calculation"]] = "tax.calculation"

    class CustomerDetails(StripeObject):
        class Address(StripeObject):
            city: Optional[str]
            """
            City, district, suburb, town, or village.
            """
            country: str
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
            State/province as an [ISO 3166-2](https://en.wikipedia.org/wiki/ISO_3166-2) subdivision code, without country prefix. Example: "NY" or "TX".
            """

        class TaxId(StripeObject):
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
                "unknown",
                "us_ein",
                "uy_ruc",
                "ve_rif",
                "vn_tin",
                "za_vat",
            ]
            """
            The type of the tax ID, one of `ad_nrt`, `ar_cuit`, `eu_vat`, `bo_tin`, `br_cnpj`, `br_cpf`, `cn_tin`, `co_nit`, `cr_tin`, `do_rcn`, `ec_ruc`, `eu_oss_vat`, `pe_ruc`, `ro_tin`, `rs_pib`, `sv_nit`, `uy_ruc`, `ve_rif`, `vn_tin`, `gb_vat`, `nz_gst`, `au_abn`, `au_arn`, `in_gst`, `no_vat`, `no_voec`, `za_vat`, `ch_vat`, `mx_rfc`, `sg_uen`, `ru_inn`, `ru_kpp`, `ca_bn`, `hk_br`, `es_cif`, `tw_vat`, `th_vat`, `jp_cn`, `jp_rn`, `jp_trn`, `li_uid`, `my_itn`, `us_ein`, `kr_brn`, `ca_qst`, `ca_gst_hst`, `ca_pst_bc`, `ca_pst_mb`, `ca_pst_sk`, `my_sst`, `sg_gst`, `ae_trn`, `cl_tin`, `sa_vat`, `id_npwp`, `my_frp`, `il_vat`, `ge_vat`, `ua_vat`, `is_vat`, `bg_uic`, `hu_tin`, `si_tin`, `ke_pin`, `tr_tin`, `eg_tin`, `ph_tin`, `bh_vat`, `kz_bin`, `ng_tin`, `om_vat`, `de_stn`, `ch_uid`, or `unknown`
            """
            value: str
            """
            The value of the tax ID.
            """

        address: Optional[Address]
        """
        The customer's postal address (for example, home or business location).
        """
        address_source: Optional[Literal["billing", "shipping"]]
        """
        The type of customer address provided.
        """
        ip_address: Optional[str]
        """
        The customer's IP address (IPv4 or IPv6).
        """
        tax_ids: List[TaxId]
        """
        The customer's tax IDs (for example, EU VAT numbers).
        """
        taxability_override: Literal[
            "customer_exempt", "none", "reverse_charge"
        ]
        """
        The taxability override used for taxation.
        """
        _inner_class_types = {"address": Address, "tax_ids": TaxId}

    class ShipFromDetails(StripeObject):
        class Address(StripeObject):
            city: Optional[str]
            """
            City, district, suburb, town, or village.
            """
            country: str
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
            State/province as an [ISO 3166-2](https://en.wikipedia.org/wiki/ISO_3166-2) subdivision code, without country prefix. Example: "NY" or "TX".
            """

        address: Address
        _inner_class_types = {"address": Address}

    class ShippingCost(StripeObject):
        class TaxBreakdown(StripeObject):
            class Jurisdiction(StripeObject):
                country: str
                """
                Two-letter country code ([ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)).
                """
                display_name: str
                """
                A human-readable name for the jurisdiction imposing the tax.
                """
                level: Literal[
                    "city", "country", "county", "district", "state"
                ]
                """
                Indicates the level of the jurisdiction imposing the tax.
                """
                state: Optional[str]
                """
                [ISO 3166-2 subdivision code](https://en.wikipedia.org/wiki/ISO_3166-2:US), without country prefix. For example, "NY" for New York, United States.
                """

            class TaxRateDetails(StripeObject):
                display_name: str
                """
                A localized display name for tax type, intended to be human-readable. For example, "Local Sales and Use Tax", "Value-added tax (VAT)", or "Umsatzsteuer (USt.)".
                """
                percentage_decimal: str
                """
                The tax rate percentage as a string. For example, 8.5% is represented as "8.5".
                """
                tax_type: Literal[
                    "amusement_tax",
                    "communications_tax",
                    "gst",
                    "hst",
                    "igst",
                    "jct",
                    "lease_tax",
                    "pst",
                    "qst",
                    "rst",
                    "sales_tax",
                    "vat",
                ]
                """
                The tax type, such as `vat` or `sales_tax`.
                """

            amount: int
            """
            The amount of tax, in the [smallest currency unit](https://stripe.com/docs/currencies#zero-decimal).
            """
            jurisdiction: Jurisdiction
            sourcing: Literal["destination", "origin"]
            """
            Indicates whether the jurisdiction was determined by the origin (merchant's address) or destination (customer's address).
            """
            tax_rate_details: Optional[TaxRateDetails]
            """
            Details regarding the rate for this tax. This field will be `null` when the tax is not imposed, for example if the product is exempt from tax.
            """
            taxability_reason: Literal[
                "customer_exempt",
                "not_collecting",
                "not_subject_to_tax",
                "not_supported",
                "portion_product_exempt",
                "portion_reduced_rated",
                "portion_standard_rated",
                "product_exempt",
                "product_exempt_holiday",
                "proportionally_rated",
                "reduced_rated",
                "reverse_charge",
                "standard_rated",
                "taxable_basis_reduced",
                "zero_rated",
            ]
            """
            The reasoning behind this tax, for example, if the product is tax exempt. The possible values for this field may be extended as new tax rules are supported.
            """
            taxable_amount: int
            """
            The amount on which tax is calculated, in the [smallest currency unit](https://stripe.com/docs/currencies#zero-decimal).
            """
            _inner_class_types = {
                "jurisdiction": Jurisdiction,
                "tax_rate_details": TaxRateDetails,
            }

        amount: int
        """
        The shipping amount in the [smallest currency unit](https://stripe.com/docs/currencies#zero-decimal). If `tax_behavior=inclusive`, then this amount includes taxes. Otherwise, taxes were calculated on top of this amount.
        """
        amount_tax: int
        """
        The amount of tax calculated for shipping, in the [smallest currency unit](https://stripe.com/docs/currencies#zero-decimal).
        """
        shipping_rate: Optional[str]
        """
        The ID of an existing [ShippingRate](https://stripe.com/docs/api/shipping_rates/object).
        """
        tax_behavior: Literal["exclusive", "inclusive"]
        """
        Specifies whether the `amount` includes taxes. If `tax_behavior=inclusive`, then the amount includes taxes.
        """
        tax_breakdown: Optional[List[TaxBreakdown]]
        """
        Detailed account of taxes relevant to shipping cost.
        """
        tax_code: str
        """
        The [tax code](https://stripe.com/docs/tax/tax-categories) ID used for shipping.
        """
        _inner_class_types = {"tax_breakdown": TaxBreakdown}

    class TaxBreakdown(StripeObject):
        class TaxRateDetails(StripeObject):
            country: Optional[str]
            """
            Two-letter country code ([ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)).
            """
            percentage_decimal: str
            """
            The tax rate percentage as a string. For example, 8.5% is represented as `"8.5"`.
            """
            state: Optional[str]
            """
            State, county, province, or region.
            """
            tax_type: Optional[
                Literal[
                    "amusement_tax",
                    "communications_tax",
                    "gst",
                    "hst",
                    "igst",
                    "jct",
                    "lease_tax",
                    "pst",
                    "qst",
                    "rst",
                    "sales_tax",
                    "vat",
                ]
            ]
            """
            The tax type, such as `vat` or `sales_tax`.
            """

        amount: int
        """
        The amount of tax, in the [smallest currency unit](https://stripe.com/docs/currencies#zero-decimal).
        """
        inclusive: bool
        """
        Specifies whether the tax amount is included in the line item amount.
        """
        tax_rate_details: TaxRateDetails
        taxability_reason: Literal[
            "customer_exempt",
            "not_collecting",
            "not_subject_to_tax",
            "not_supported",
            "portion_product_exempt",
            "portion_reduced_rated",
            "portion_standard_rated",
            "product_exempt",
            "product_exempt_holiday",
            "proportionally_rated",
            "reduced_rated",
            "reverse_charge",
            "standard_rated",
            "taxable_basis_reduced",
            "zero_rated",
        ]
        """
        The reasoning behind this tax, for example, if the product is tax exempt. We might extend the possible values for this field to support new tax rules.
        """
        taxable_amount: int
        """
        The amount on which tax is calculated, in the [smallest currency unit](https://stripe.com/docs/currencies#zero-decimal).
        """
        _inner_class_types = {"tax_rate_details": TaxRateDetails}

    class CreateParams(RequestOptions):
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        customer: NotRequired[str]
        """
        The ID of an existing customer to use for this calculation. If provided, the customer's address and tax IDs are copied to `customer_details`.
        """
        customer_details: NotRequired[
            "Calculation.CreateParamsCustomerDetails"
        ]
        """
        Details about the customer, including address and tax IDs.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        line_items: List["Calculation.CreateParamsLineItem"]
        """
        A list of items the customer is purchasing.
        """
        ship_from_details: NotRequired[
            "Calculation.CreateParamsShipFromDetails"
        ]
        """
        Details about the address from which the goods are being shipped.
        """
        shipping_cost: NotRequired["Calculation.CreateParamsShippingCost"]
        """
        Shipping cost details to be used for the calculation.
        """
        tax_date: NotRequired[int]
        """
        Timestamp of date at which the tax rules and rates in effect applies for the calculation. Measured in seconds since the Unix epoch. Can be up to 48 hours in the past, and up to 48 hours in the future.
        """

    class CreateParamsCustomerDetails(TypedDict):
        address: NotRequired["Calculation.CreateParamsCustomerDetailsAddress"]
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
            List["Calculation.CreateParamsCustomerDetailsTaxId"]
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
        address: "Calculation.CreateParamsShipFromDetailsAddress"
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

    class ListLineItemsParams(RequestOptions):
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

    class RetrieveParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    amount_total: int
    """
    Total amount after taxes in the [smallest currency unit](https://stripe.com/docs/currencies#zero-decimal).
    """
    currency: str
    """
    Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
    """
    customer: Optional[str]
    """
    The ID of an existing [Customer](https://stripe.com/docs/api/customers/object) used for the resource.
    """
    customer_details: CustomerDetails
    expires_at: Optional[int]
    """
    Timestamp of date at which the tax calculation will expire.
    """
    id: Optional[str]
    """
    Unique identifier for the calculation.
    """
    line_items: Optional[ListObject["CalculationLineItem"]]
    """
    The list of items the customer is purchasing.
    """
    livemode: bool
    """
    Has the value `true` if the object exists in live mode or the value `false` if the object exists in test mode.
    """
    object: Literal["tax.calculation"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """
    ship_from_details: Optional[ShipFromDetails]
    """
    The details of the ship from location, such as the address.
    """
    shipping_cost: Optional[ShippingCost]
    """
    The shipping cost details for the calculation.
    """
    tax_amount_exclusive: int
    """
    The amount of tax to be collected on top of the line item prices.
    """
    tax_amount_inclusive: int
    """
    The amount of tax already included in the line item prices.
    """
    tax_breakdown: List[TaxBreakdown]
    """
    Breakdown of individual tax amounts that add up to the total.
    """
    tax_date: int
    """
    Timestamp of date at which the tax rules and rates in effect applies for the calculation.
    """

    @classmethod
    def create(
        cls, **params: Unpack["Calculation.CreateParams"]
    ) -> "Calculation":
        """
        Calculates tax based on the input and returns a Tax Calculation object.
        """
        return cast(
            "Calculation",
            cls._static_request(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    async def create_async(
        cls, **params: Unpack["Calculation.CreateParams"]
    ) -> "Calculation":
        """
        Calculates tax based on the input and returns a Tax Calculation object.
        """
        return cast(
            "Calculation",
            await cls._static_request_async(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    def _cls_list_line_items(
        cls,
        calculation: str,
        **params: Unpack["Calculation.ListLineItemsParams"],
    ) -> ListObject["CalculationLineItem"]:
        """
        Retrieves the line items of a tax calculation as a collection, if the calculation hasn't expired.
        """
        return cast(
            ListObject["CalculationLineItem"],
            cls._static_request(
                "get",
                "/v1/tax/calculations/{calculation}/line_items".format(
                    calculation=sanitize_id(calculation)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    def list_line_items(
        calculation: str, **params: Unpack["Calculation.ListLineItemsParams"]
    ) -> ListObject["CalculationLineItem"]:
        """
        Retrieves the line items of a tax calculation as a collection, if the calculation hasn't expired.
        """
        ...

    @overload
    def list_line_items(
        self, **params: Unpack["Calculation.ListLineItemsParams"]
    ) -> ListObject["CalculationLineItem"]:
        """
        Retrieves the line items of a tax calculation as a collection, if the calculation hasn't expired.
        """
        ...

    @class_method_variant("_cls_list_line_items")
    def list_line_items(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Calculation.ListLineItemsParams"]
    ) -> ListObject["CalculationLineItem"]:
        """
        Retrieves the line items of a tax calculation as a collection, if the calculation hasn't expired.
        """
        return cast(
            ListObject["CalculationLineItem"],
            self._request(
                "get",
                "/v1/tax/calculations/{calculation}/line_items".format(
                    calculation=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    async def _cls_list_line_items_async(
        cls,
        calculation: str,
        **params: Unpack["Calculation.ListLineItemsParams"],
    ) -> ListObject["CalculationLineItem"]:
        """
        Retrieves the line items of a tax calculation as a collection, if the calculation hasn't expired.
        """
        return cast(
            ListObject["CalculationLineItem"],
            await cls._static_request_async(
                "get",
                "/v1/tax/calculations/{calculation}/line_items".format(
                    calculation=sanitize_id(calculation)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    async def list_line_items_async(
        calculation: str, **params: Unpack["Calculation.ListLineItemsParams"]
    ) -> ListObject["CalculationLineItem"]:
        """
        Retrieves the line items of a tax calculation as a collection, if the calculation hasn't expired.
        """
        ...

    @overload
    async def list_line_items_async(
        self, **params: Unpack["Calculation.ListLineItemsParams"]
    ) -> ListObject["CalculationLineItem"]:
        """
        Retrieves the line items of a tax calculation as a collection, if the calculation hasn't expired.
        """
        ...

    @class_method_variant("_cls_list_line_items_async")
    async def list_line_items_async(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Calculation.ListLineItemsParams"]
    ) -> ListObject["CalculationLineItem"]:
        """
        Retrieves the line items of a tax calculation as a collection, if the calculation hasn't expired.
        """
        return cast(
            ListObject["CalculationLineItem"],
            await self._request_async(
                "get",
                "/v1/tax/calculations/{calculation}/line_items".format(
                    calculation=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    def retrieve(
        cls, id: str, **params: Unpack["Calculation.RetrieveParams"]
    ) -> "Calculation":
        """
        Retrieves a Tax Calculation object, if the calculation hasn't expired.
        """
        instance = cls(id, **params)
        instance.refresh()
        return instance

    @classmethod
    async def retrieve_async(
        cls, id: str, **params: Unpack["Calculation.RetrieveParams"]
    ) -> "Calculation":
        """
        Retrieves a Tax Calculation object, if the calculation hasn't expired.
        """
        instance = cls(id, **params)
        await instance.refresh_async()
        return instance

    _inner_class_types = {
        "customer_details": CustomerDetails,
        "ship_from_details": ShipFromDetails,
        "shipping_cost": ShippingCost,
        "tax_breakdown": TaxBreakdown,
    }

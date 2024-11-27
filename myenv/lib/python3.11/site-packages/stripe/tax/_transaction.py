# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._api_resource import APIResource
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_object import StripeObject
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
    from stripe.tax._transaction_line_item import TransactionLineItem


class Transaction(APIResource["Transaction"]):
    """
    A Tax Transaction records the tax collected from or refunded to your customer.

    Related guide: [Calculate tax in your custom payment flow](https://stripe.com/docs/tax/custom#tax-transaction)
    """

    OBJECT_NAME: ClassVar[Literal["tax.transaction"]] = "tax.transaction"

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

    class Reversal(StripeObject):
        original_transaction: Optional[str]
        """
        The `id` of the reversed `Transaction` object.
        """

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
        Detailed account of taxes relevant to shipping cost. (It is not populated for the transaction resource object and will be removed in the next API version.)
        """
        tax_code: str
        """
        The [tax code](https://stripe.com/docs/tax/tax-categories) ID used for shipping.
        """
        _inner_class_types = {"tax_breakdown": TaxBreakdown}

    class CreateFromCalculationParams(RequestOptions):
        calculation: str
        """
        Tax Calculation ID to be used as input when creating the transaction.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        posted_at: NotRequired[int]
        """
        The Unix timestamp representing when the tax liability is assumed or reduced, which determines the liability posting period and handling in tax liability reports. The timestamp must fall within the `tax_date` and the current time, unless the `tax_date` is scheduled in advance. Defaults to the current time.
        """
        reference: str
        """
        A custom order or sale identifier, such as 'myOrder_123'. Must be unique across all transactions, including reversals.
        """

    class CreateReversalParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        flat_amount: NotRequired[int]
        """
        A flat amount to reverse across the entire transaction, in the [smallest currency unit](https://stripe.com/docs/currencies#zero-decimal) in negative. This value represents the total amount to refund from the transaction, including taxes.
        """
        line_items: NotRequired[
            List["Transaction.CreateReversalParamsLineItem"]
        ]
        """
        The line item amounts to reverse.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        mode: Literal["full", "partial"]
        """
        If `partial`, the provided line item or shipping cost amounts are reversed. If `full`, the original transaction is fully reversed.
        """
        original_transaction: str
        """
        The ID of the Transaction to partially or fully reverse.
        """
        reference: str
        """
        A custom identifier for this reversal, such as `myOrder_123-refund_1`, which must be unique across all transactions. The reference helps identify this reversal transaction in exported [tax reports](https://stripe.com/docs/tax/reports).
        """
        shipping_cost: NotRequired[
            "Transaction.CreateReversalParamsShippingCost"
        ]
        """
        The shipping cost to reverse.
        """

    class CreateReversalParamsLineItem(TypedDict):
        amount: int
        """
        The amount to reverse, in the [smallest currency unit](https://stripe.com/docs/currencies#zero-decimal) in negative.
        """
        amount_tax: int
        """
        The amount of tax to reverse, in the [smallest currency unit](https://stripe.com/docs/currencies#zero-decimal) in negative.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format.
        """
        original_line_item: str
        """
        The `id` of the line item to reverse in the original transaction.
        """
        quantity: NotRequired[int]
        """
        The quantity reversed. Appears in [tax exports](https://stripe.com/docs/tax/reports), but does not affect the amount of tax reversed.
        """
        reference: str
        """
        A custom identifier for this line item in the reversal transaction, such as 'L1-refund'.
        """

    class CreateReversalParamsShippingCost(TypedDict):
        amount: int
        """
        The amount to reverse, in the [smallest currency unit](https://stripe.com/docs/currencies#zero-decimal) in negative.
        """
        amount_tax: int
        """
        The amount of tax to reverse, in the [smallest currency unit](https://stripe.com/docs/currencies#zero-decimal) in negative.
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
    The ID of an existing [Customer](https://stripe.com/docs/api/customers/object) used for the resource.
    """
    customer_details: CustomerDetails
    id: str
    """
    Unique identifier for the transaction.
    """
    line_items: Optional[ListObject["TransactionLineItem"]]
    """
    The tax collected or refunded, by line item.
    """
    livemode: bool
    """
    Has the value `true` if the object exists in live mode or the value `false` if the object exists in test mode.
    """
    metadata: Optional[Dict[str, str]]
    """
    Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format.
    """
    object: Literal["tax.transaction"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """
    posted_at: int
    """
    The Unix timestamp representing when the tax liability is assumed or reduced.
    """
    reference: str
    """
    A custom unique identifier, such as 'myOrder_123'.
    """
    reversal: Optional[Reversal]
    """
    If `type=reversal`, contains information about what was reversed.
    """
    ship_from_details: Optional[ShipFromDetails]
    """
    The details of the ship from location, such as the address.
    """
    shipping_cost: Optional[ShippingCost]
    """
    The shipping cost details for the transaction.
    """
    tax_date: int
    """
    Timestamp of date at which the tax rules and rates in effect applies for the calculation.
    """
    type: Literal["reversal", "transaction"]
    """
    If `reversal`, this transaction reverses an earlier transaction.
    """

    @classmethod
    def create_from_calculation(
        cls, **params: Unpack["Transaction.CreateFromCalculationParams"]
    ) -> "Transaction":
        """
        Creates a Tax Transaction from a calculation, if that calculation hasn't expired. Calculations expire after 90 days.
        """
        return cast(
            "Transaction",
            cls._static_request(
                "post",
                "/v1/tax/transactions/create_from_calculation",
                params=params,
            ),
        )

    @classmethod
    async def create_from_calculation_async(
        cls, **params: Unpack["Transaction.CreateFromCalculationParams"]
    ) -> "Transaction":
        """
        Creates a Tax Transaction from a calculation, if that calculation hasn't expired. Calculations expire after 90 days.
        """
        return cast(
            "Transaction",
            await cls._static_request_async(
                "post",
                "/v1/tax/transactions/create_from_calculation",
                params=params,
            ),
        )

    @classmethod
    def create_reversal(
        cls, **params: Unpack["Transaction.CreateReversalParams"]
    ) -> "Transaction":
        """
        Partially or fully reverses a previously created Transaction.
        """
        return cast(
            "Transaction",
            cls._static_request(
                "post",
                "/v1/tax/transactions/create_reversal",
                params=params,
            ),
        )

    @classmethod
    async def create_reversal_async(
        cls, **params: Unpack["Transaction.CreateReversalParams"]
    ) -> "Transaction":
        """
        Partially or fully reverses a previously created Transaction.
        """
        return cast(
            "Transaction",
            await cls._static_request_async(
                "post",
                "/v1/tax/transactions/create_reversal",
                params=params,
            ),
        )

    @classmethod
    def _cls_list_line_items(
        cls,
        transaction: str,
        **params: Unpack["Transaction.ListLineItemsParams"],
    ) -> ListObject["TransactionLineItem"]:
        """
        Retrieves the line items of a committed standalone transaction as a collection.
        """
        return cast(
            ListObject["TransactionLineItem"],
            cls._static_request(
                "get",
                "/v1/tax/transactions/{transaction}/line_items".format(
                    transaction=sanitize_id(transaction)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    def list_line_items(
        transaction: str, **params: Unpack["Transaction.ListLineItemsParams"]
    ) -> ListObject["TransactionLineItem"]:
        """
        Retrieves the line items of a committed standalone transaction as a collection.
        """
        ...

    @overload
    def list_line_items(
        self, **params: Unpack["Transaction.ListLineItemsParams"]
    ) -> ListObject["TransactionLineItem"]:
        """
        Retrieves the line items of a committed standalone transaction as a collection.
        """
        ...

    @class_method_variant("_cls_list_line_items")
    def list_line_items(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Transaction.ListLineItemsParams"]
    ) -> ListObject["TransactionLineItem"]:
        """
        Retrieves the line items of a committed standalone transaction as a collection.
        """
        return cast(
            ListObject["TransactionLineItem"],
            self._request(
                "get",
                "/v1/tax/transactions/{transaction}/line_items".format(
                    transaction=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    async def _cls_list_line_items_async(
        cls,
        transaction: str,
        **params: Unpack["Transaction.ListLineItemsParams"],
    ) -> ListObject["TransactionLineItem"]:
        """
        Retrieves the line items of a committed standalone transaction as a collection.
        """
        return cast(
            ListObject["TransactionLineItem"],
            await cls._static_request_async(
                "get",
                "/v1/tax/transactions/{transaction}/line_items".format(
                    transaction=sanitize_id(transaction)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    async def list_line_items_async(
        transaction: str, **params: Unpack["Transaction.ListLineItemsParams"]
    ) -> ListObject["TransactionLineItem"]:
        """
        Retrieves the line items of a committed standalone transaction as a collection.
        """
        ...

    @overload
    async def list_line_items_async(
        self, **params: Unpack["Transaction.ListLineItemsParams"]
    ) -> ListObject["TransactionLineItem"]:
        """
        Retrieves the line items of a committed standalone transaction as a collection.
        """
        ...

    @class_method_variant("_cls_list_line_items_async")
    async def list_line_items_async(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Transaction.ListLineItemsParams"]
    ) -> ListObject["TransactionLineItem"]:
        """
        Retrieves the line items of a committed standalone transaction as a collection.
        """
        return cast(
            ListObject["TransactionLineItem"],
            await self._request_async(
                "get",
                "/v1/tax/transactions/{transaction}/line_items".format(
                    transaction=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    def retrieve(
        cls, id: str, **params: Unpack["Transaction.RetrieveParams"]
    ) -> "Transaction":
        """
        Retrieves a Tax Transaction object.
        """
        instance = cls(id, **params)
        instance.refresh()
        return instance

    @classmethod
    async def retrieve_async(
        cls, id: str, **params: Unpack["Transaction.RetrieveParams"]
    ) -> "Transaction":
        """
        Retrieves a Tax Transaction object.
        """
        instance = cls(id, **params)
        await instance.refresh_async()
        return instance

    _inner_class_types = {
        "customer_details": CustomerDetails,
        "reversal": Reversal,
        "ship_from_details": ShipFromDetails,
        "shipping_cost": ShippingCost,
    }

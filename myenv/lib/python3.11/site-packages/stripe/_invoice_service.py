# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._invoice import Invoice
from stripe._invoice_line_item_service import InvoiceLineItemService
from stripe._invoice_upcoming_lines_service import InvoiceUpcomingLinesService
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._search_result_object import SearchResultObject
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from typing import Dict, List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class InvoiceService(StripeService):
    def __init__(self, requestor):
        super().__init__(requestor)
        self.line_items = InvoiceLineItemService(self._requestor)
        self.upcoming_lines = InvoiceUpcomingLinesService(self._requestor)

    class AddLinesParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        invoice_metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        lines: List["InvoiceService.AddLinesParamsLine"]
        """
        The line items to add.
        """

    class AddLinesParamsLine(TypedDict):
        amount: NotRequired[int]
        """
        The integer amount in cents (or local equivalent) of the charge to be applied to the upcoming invoice. If you want to apply a credit to the customer's account, pass a negative amount.
        """
        description: NotRequired[str]
        """
        An arbitrary string which you can attach to the invoice item. The description is displayed in the invoice for easy tracking.
        """
        discountable: NotRequired[bool]
        """
        Controls whether discounts apply to this line item. Defaults to false for prorations or negative line items, and true for all other line items. Cannot be set to true for prorations.
        """
        discounts: NotRequired[
            "Literal['']|List[InvoiceService.AddLinesParamsLineDiscount]"
        ]
        """
        The coupons, promotion codes & existing discounts which apply to the line item. Item discounts are applied before invoice discounts. Pass an empty string to remove previously-defined discounts.
        """
        invoice_item: NotRequired[str]
        """
        ID of an unassigned invoice item to assign to this invoice. If not provided, a new item will be created.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        period: NotRequired["InvoiceService.AddLinesParamsLinePeriod"]
        """
        The period associated with this invoice item. When set to different values, the period will be rendered on the invoice. If you have [Stripe Revenue Recognition](https://stripe.com/docs/revenue-recognition) enabled, the period will be used to recognize and defer revenue. See the [Revenue Recognition documentation](https://stripe.com/docs/revenue-recognition/methodology/subscriptions-and-invoicing) for details.
        """
        price: NotRequired[str]
        """
        The ID of the price object. One of `price` or `price_data` is required.
        """
        price_data: NotRequired["InvoiceService.AddLinesParamsLinePriceData"]
        """
        Data used to generate a new [Price](https://stripe.com/docs/api/prices) object inline. One of `price` or `price_data` is required.
        """
        quantity: NotRequired[int]
        """
        Non-negative integer. The quantity of units for the line item.
        """
        tax_amounts: NotRequired[
            "Literal['']|List[InvoiceService.AddLinesParamsLineTaxAmount]"
        ]
        """
        A list of up to 10 tax amounts for this line item. This can be useful if you calculate taxes on your own or use a third-party to calculate them. You cannot set tax amounts if any line item has [tax_rates](https://stripe.com/docs/api/invoices/line_item#invoice_line_item_object-tax_rates) or if the invoice has [default_tax_rates](https://stripe.com/docs/api/invoices/object#invoice_object-default_tax_rates) or uses [automatic tax](https://stripe.com/docs/tax/invoicing). Pass an empty string to remove previously defined tax amounts.
        """
        tax_rates: NotRequired["Literal['']|List[str]"]
        """
        The tax rates which apply to the line item. When set, the `default_tax_rates` on the invoice do not apply to this line item. Pass an empty string to remove previously-defined tax rates.
        """

    class AddLinesParamsLineDiscount(TypedDict):
        coupon: NotRequired[str]
        """
        ID of the coupon to create a new discount for.
        """
        discount: NotRequired[str]
        """
        ID of an existing discount on the object (or one of its ancestors) to reuse.
        """
        promotion_code: NotRequired[str]
        """
        ID of the promotion code to create a new discount for.
        """

    class AddLinesParamsLinePeriod(TypedDict):
        end: int
        """
        The end of the period, which must be greater than or equal to the start. This value is inclusive.
        """
        start: int
        """
        The start of the period. This value is inclusive.
        """

    class AddLinesParamsLinePriceData(TypedDict):
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        product: NotRequired[str]
        """
        The ID of the product that this price will belong to. One of `product` or `product_data` is required.
        """
        product_data: NotRequired[
            "InvoiceService.AddLinesParamsLinePriceDataProductData"
        ]
        """
        Data used to generate a new product object inline. One of `product` or `product_data` is required.
        """
        tax_behavior: NotRequired[
            Literal["exclusive", "inclusive", "unspecified"]
        ]
        """
        Only required if a [default tax behavior](https://stripe.com/docs/tax/products-prices-tax-categories-tax-behavior#setting-a-default-tax-behavior-(recommended)) was not provided in the Stripe Tax settings. Specifies whether the price is considered inclusive of taxes or exclusive of taxes. One of `inclusive`, `exclusive`, or `unspecified`. Once specified as either `inclusive` or `exclusive`, it cannot be changed.
        """
        unit_amount: NotRequired[int]
        """
        A non-negative integer in cents (or local equivalent) representing how much to charge. One of `unit_amount` or `unit_amount_decimal` is required.
        """
        unit_amount_decimal: NotRequired[str]
        """
        Same as `unit_amount`, but accepts a decimal value in cents (or local equivalent) with at most 12 decimal places. Only one of `unit_amount` and `unit_amount_decimal` can be set.
        """

    class AddLinesParamsLinePriceDataProductData(TypedDict):
        description: NotRequired[str]
        """
        The product's description, meant to be displayable to the customer. Use this field to optionally store a long form explanation of the product being sold for your own rendering purposes.
        """
        images: NotRequired[List[str]]
        """
        A list of up to 8 URLs of images for this product, meant to be displayable to the customer.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        name: str
        """
        The product's name, meant to be displayable to the customer.
        """
        tax_code: NotRequired[str]
        """
        A [tax code](https://stripe.com/docs/tax/tax-categories) ID.
        """

    class AddLinesParamsLineTaxAmount(TypedDict):
        amount: int
        """
        The amount, in cents (or local equivalent), of the tax.
        """
        tax_rate_data: "InvoiceService.AddLinesParamsLineTaxAmountTaxRateData"
        """
        Data to find or create a TaxRate object.

        Stripe automatically creates or reuses a TaxRate object for each tax amount. If the `tax_rate_data` exactly matches a previous value, Stripe will reuse the TaxRate object. TaxRate objects created automatically by Stripe are immediately archived, do not appear in the line item's `tax_rates`, and cannot be directly added to invoices, payments, or line items.
        """
        taxable_amount: int
        """
        The amount on which tax is calculated, in cents (or local equivalent).
        """

    class AddLinesParamsLineTaxAmountTaxRateData(TypedDict):
        country: NotRequired[str]
        """
        Two-letter country code ([ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)).
        """
        description: NotRequired[str]
        """
        An arbitrary string attached to the tax rate for your internal use only. It will not be visible to your customers.
        """
        display_name: str
        """
        The display name of the tax rate, which will be shown to users.
        """
        inclusive: bool
        """
        This specifies if the tax rate is inclusive or exclusive.
        """
        jurisdiction: NotRequired[str]
        """
        The jurisdiction for the tax rate. You can use this label field for tax reporting purposes. It also appears on your customer's invoice.
        """
        percentage: float
        """
        The statutory tax rate percent. This field accepts decimal values between 0 and 100 inclusive with at most 4 decimal places. To accommodate fixed-amount taxes, set the percentage to zero. Stripe will not display zero percentages on the invoice unless the `amount` of the tax is also zero.
        """
        state: NotRequired[str]
        """
        [ISO 3166-2 subdivision code](https://en.wikipedia.org/wiki/ISO_3166-2:US), without country prefix. For example, "NY" for New York, United States.
        """
        tax_type: NotRequired[
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
        The high-level tax type, such as `vat` or `sales_tax`.
        """

    class CreateParams(TypedDict):
        account_tax_ids: NotRequired["Literal['']|List[str]"]
        """
        The account tax IDs associated with the invoice. Only editable when the invoice is a draft.
        """
        application_fee_amount: NotRequired[int]
        """
        A fee in cents (or local equivalent) that will be applied to the invoice and transferred to the application owner's Stripe account. The request must be made with an OAuth key or the Stripe-Account header in order to take an application fee. For more information, see the application fees [documentation](https://stripe.com/docs/billing/invoices/connect#collecting-fees).
        """
        auto_advance: NotRequired[bool]
        """
        Controls whether Stripe performs [automatic collection](https://stripe.com/docs/invoicing/integration/automatic-advancement-collection) of the invoice. If `false`, the invoice's state doesn't automatically advance without an explicit action.
        """
        automatic_tax: NotRequired["InvoiceService.CreateParamsAutomaticTax"]
        """
        Settings for automatic tax lookup for this invoice.
        """
        collection_method: NotRequired[
            Literal["charge_automatically", "send_invoice"]
        ]
        """
        Either `charge_automatically`, or `send_invoice`. When charging automatically, Stripe will attempt to pay this invoice using the default source attached to the customer. When sending an invoice, Stripe will email this invoice to the customer with payment instructions. Defaults to `charge_automatically`.
        """
        currency: NotRequired[str]
        """
        The currency to create this invoice in. Defaults to that of `customer` if not specified.
        """
        custom_fields: NotRequired[
            "Literal['']|List[InvoiceService.CreateParamsCustomField]"
        ]
        """
        A list of up to 4 custom fields to be displayed on the invoice.
        """
        customer: NotRequired[str]
        """
        The ID of the customer who will be billed.
        """
        days_until_due: NotRequired[int]
        """
        The number of days from when the invoice is created until it is due. Valid only for invoices where `collection_method=send_invoice`.
        """
        default_payment_method: NotRequired[str]
        """
        ID of the default payment method for the invoice. It must belong to the customer associated with the invoice. If not set, defaults to the subscription's default payment method, if any, or to the default payment method in the customer's invoice settings.
        """
        default_source: NotRequired[str]
        """
        ID of the default payment source for the invoice. It must belong to the customer associated with the invoice and be in a chargeable state. If not set, defaults to the subscription's default source, if any, or to the customer's default source.
        """
        default_tax_rates: NotRequired[List[str]]
        """
        The tax rates that will apply to any line item that does not have `tax_rates` set.
        """
        description: NotRequired[str]
        """
        An arbitrary string attached to the object. Often useful for displaying to users. Referenced as 'memo' in the Dashboard.
        """
        discounts: NotRequired[
            "Literal['']|List[InvoiceService.CreateParamsDiscount]"
        ]
        """
        The coupons and promotion codes to redeem into discounts for the invoice. If not specified, inherits the discount from the invoice's customer. Pass an empty string to avoid inheriting any discounts.
        """
        due_date: NotRequired[int]
        """
        The date on which payment for this invoice is due. Valid only for invoices where `collection_method=send_invoice`.
        """
        effective_at: NotRequired[int]
        """
        The date when this invoice is in effect. Same as `finalized_at` unless overwritten. When defined, this value replaces the system-generated 'Date of issue' printed on the invoice PDF and receipt.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        footer: NotRequired[str]
        """
        Footer to be displayed on the invoice.
        """
        from_invoice: NotRequired["InvoiceService.CreateParamsFromInvoice"]
        """
        Revise an existing invoice. The new invoice will be created in `status=draft`. See the [revision documentation](https://stripe.com/docs/invoicing/invoice-revisions) for more details.
        """
        issuer: NotRequired["InvoiceService.CreateParamsIssuer"]
        """
        The connected account that issues the invoice. The invoice is presented with the branding and support information of the specified account.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        number: NotRequired[str]
        """
        Set the number for this invoice. If no number is present then a number will be assigned automatically when the invoice is finalized. In many markets, regulations require invoices to be unique, sequential and / or gapless. You are responsible for ensuring this is true across all your different invoicing systems in the event that you edit the invoice number using our API. If you use only Stripe for your invoices and do not change invoice numbers, Stripe handles this aspect of compliance for you automatically.
        """
        on_behalf_of: NotRequired[str]
        """
        The account (if any) for which the funds of the invoice payment are intended. If set, the invoice will be presented with the branding and support information of the specified account. See the [Invoices with Connect](https://stripe.com/docs/billing/invoices/connect) documentation for details.
        """
        payment_settings: NotRequired[
            "InvoiceService.CreateParamsPaymentSettings"
        ]
        """
        Configuration settings for the PaymentIntent that is generated when the invoice is finalized.
        """
        pending_invoice_items_behavior: NotRequired[
            Literal["exclude", "include"]
        ]
        """
        How to handle pending invoice items on invoice creation. Defaults to `exclude` if the parameter is omitted.
        """
        rendering: NotRequired["InvoiceService.CreateParamsRendering"]
        """
        The rendering-related settings that control how the invoice is displayed on customer-facing surfaces such as PDF and Hosted Invoice Page.
        """
        shipping_cost: NotRequired["InvoiceService.CreateParamsShippingCost"]
        """
        Settings for the cost of shipping for this invoice.
        """
        shipping_details: NotRequired[
            "InvoiceService.CreateParamsShippingDetails"
        ]
        """
        Shipping details for the invoice. The Invoice PDF will use the `shipping_details` value if it is set, otherwise the PDF will render the shipping address from the customer.
        """
        statement_descriptor: NotRequired[str]
        """
        Extra information about a charge for the customer's credit card statement. It must contain at least one letter. If not specified and this invoice is part of a subscription, the default `statement_descriptor` will be set to the first subscription item's product's `statement_descriptor`.
        """
        subscription: NotRequired[str]
        """
        The ID of the subscription to invoice, if any. If set, the created invoice will only include pending invoice items for that subscription. The subscription's billing cycle and regular subscription events won't be affected.
        """
        transfer_data: NotRequired["InvoiceService.CreateParamsTransferData"]
        """
        If specified, the funds from the invoice will be transferred to the destination and the ID of the resulting transfer will be found on the invoice's charge.
        """

    class CreateParamsAutomaticTax(TypedDict):
        enabled: bool
        """
        Whether Stripe automatically computes tax on this invoice. Note that incompatible invoice items (invoice items with manually specified [tax rates](https://stripe.com/docs/api/tax_rates), negative amounts, or `tax_behavior=unspecified`) cannot be added to automatic tax invoices.
        """
        liability: NotRequired[
            "InvoiceService.CreateParamsAutomaticTaxLiability"
        ]
        """
        The account that's liable for tax. If set, the business address and tax registrations required to perform the tax calculation are loaded from this account. The tax transaction is returned in the report of the connected account.
        """

    class CreateParamsAutomaticTaxLiability(TypedDict):
        account: NotRequired[str]
        """
        The connected account being referenced when `type` is `account`.
        """
        type: Literal["account", "self"]
        """
        Type of the account referenced in the request.
        """

    class CreateParamsCustomField(TypedDict):
        name: str
        """
        The name of the custom field. This may be up to 40 characters.
        """
        value: str
        """
        The value of the custom field. This may be up to 140 characters.
        """

    class CreateParamsDiscount(TypedDict):
        coupon: NotRequired[str]
        """
        ID of the coupon to create a new discount for.
        """
        discount: NotRequired[str]
        """
        ID of an existing discount on the object (or one of its ancestors) to reuse.
        """
        promotion_code: NotRequired[str]
        """
        ID of the promotion code to create a new discount for.
        """

    class CreateParamsFromInvoice(TypedDict):
        action: Literal["revision"]
        """
        The relation between the new invoice and the original invoice. Currently, only 'revision' is permitted
        """
        invoice: str
        """
        The `id` of the invoice that will be cloned.
        """

    class CreateParamsIssuer(TypedDict):
        account: NotRequired[str]
        """
        The connected account being referenced when `type` is `account`.
        """
        type: Literal["account", "self"]
        """
        Type of the account referenced in the request.
        """

    class CreateParamsPaymentSettings(TypedDict):
        default_mandate: NotRequired["Literal['']|str"]
        """
        ID of the mandate to be used for this invoice. It must correspond to the payment method used to pay the invoice, including the invoice's default_payment_method or default_source, if set.
        """
        payment_method_options: NotRequired[
            "InvoiceService.CreateParamsPaymentSettingsPaymentMethodOptions"
        ]
        """
        Payment-method-specific configuration to provide to the invoice's PaymentIntent.
        """
        payment_method_types: NotRequired[
            "Literal['']|List[Literal['ach_credit_transfer', 'ach_debit', 'acss_debit', 'amazon_pay', 'au_becs_debit', 'bacs_debit', 'bancontact', 'boleto', 'card', 'cashapp', 'customer_balance', 'eps', 'fpx', 'giropay', 'grabpay', 'ideal', 'konbini', 'link', 'multibanco', 'p24', 'paynow', 'paypal', 'promptpay', 'revolut_pay', 'sepa_credit_transfer', 'sepa_debit', 'sofort', 'swish', 'us_bank_account', 'wechat_pay']]"
        ]
        """
        The list of payment method types (e.g. card) to provide to the invoice's PaymentIntent. If not set, Stripe attempts to automatically determine the types to use by looking at the invoice's default payment method, the subscription's default payment method, the customer's default payment method, and your [invoice template settings](https://dashboard.stripe.com/settings/billing/invoice).
        """

    class CreateParamsPaymentSettingsPaymentMethodOptions(TypedDict):
        acss_debit: NotRequired[
            "Literal['']|InvoiceService.CreateParamsPaymentSettingsPaymentMethodOptionsAcssDebit"
        ]
        """
        If paying by `acss_debit`, this sub-hash contains details about the Canadian pre-authorized debit payment method options to pass to the invoice's PaymentIntent.
        """
        bancontact: NotRequired[
            "Literal['']|InvoiceService.CreateParamsPaymentSettingsPaymentMethodOptionsBancontact"
        ]
        """
        If paying by `bancontact`, this sub-hash contains details about the Bancontact payment method options to pass to the invoice's PaymentIntent.
        """
        card: NotRequired[
            "Literal['']|InvoiceService.CreateParamsPaymentSettingsPaymentMethodOptionsCard"
        ]
        """
        If paying by `card`, this sub-hash contains details about the Card payment method options to pass to the invoice's PaymentIntent.
        """
        customer_balance: NotRequired[
            "Literal['']|InvoiceService.CreateParamsPaymentSettingsPaymentMethodOptionsCustomerBalance"
        ]
        """
        If paying by `customer_balance`, this sub-hash contains details about the Bank transfer payment method options to pass to the invoice's PaymentIntent.
        """
        konbini: NotRequired[
            "Literal['']|InvoiceService.CreateParamsPaymentSettingsPaymentMethodOptionsKonbini"
        ]
        """
        If paying by `konbini`, this sub-hash contains details about the Konbini payment method options to pass to the invoice's PaymentIntent.
        """
        sepa_debit: NotRequired[
            "Literal['']|InvoiceService.CreateParamsPaymentSettingsPaymentMethodOptionsSepaDebit"
        ]
        """
        If paying by `sepa_debit`, this sub-hash contains details about the SEPA Direct Debit payment method options to pass to the invoice's PaymentIntent.
        """
        us_bank_account: NotRequired[
            "Literal['']|InvoiceService.CreateParamsPaymentSettingsPaymentMethodOptionsUsBankAccount"
        ]
        """
        If paying by `us_bank_account`, this sub-hash contains details about the ACH direct debit payment method options to pass to the invoice's PaymentIntent.
        """

    class CreateParamsPaymentSettingsPaymentMethodOptionsAcssDebit(TypedDict):
        mandate_options: NotRequired[
            "InvoiceService.CreateParamsPaymentSettingsPaymentMethodOptionsAcssDebitMandateOptions"
        ]
        """
        Additional fields for Mandate creation
        """
        verification_method: NotRequired[
            Literal["automatic", "instant", "microdeposits"]
        ]
        """
        Verification method for the intent
        """

    class CreateParamsPaymentSettingsPaymentMethodOptionsAcssDebitMandateOptions(
        TypedDict,
    ):
        transaction_type: NotRequired[Literal["business", "personal"]]
        """
        Transaction type of the mandate.
        """

    class CreateParamsPaymentSettingsPaymentMethodOptionsBancontact(TypedDict):
        preferred_language: NotRequired[Literal["de", "en", "fr", "nl"]]
        """
        Preferred language of the Bancontact authorization page that the customer is redirected to.
        """

    class CreateParamsPaymentSettingsPaymentMethodOptionsCard(TypedDict):
        installments: NotRequired[
            "InvoiceService.CreateParamsPaymentSettingsPaymentMethodOptionsCardInstallments"
        ]
        """
        Installment configuration for payments attempted on this invoice (Mexico Only).

        For more information, see the [installments integration guide](https://stripe.com/docs/payments/installments).
        """
        request_three_d_secure: NotRequired[
            Literal["any", "automatic", "challenge"]
        ]
        """
        We strongly recommend that you rely on our SCA Engine to automatically prompt your customers for authentication based on risk level and [other requirements](https://stripe.com/docs/strong-customer-authentication). However, if you wish to request 3D Secure based on logic from your own fraud engine, provide this option. Read our guide on [manually requesting 3D Secure](https://stripe.com/docs/payments/3d-secure/authentication-flow#manual-three-ds) for more information on how this configuration interacts with Radar and our SCA Engine.
        """

    class CreateParamsPaymentSettingsPaymentMethodOptionsCardInstallments(
        TypedDict,
    ):
        enabled: NotRequired[bool]
        """
        Setting to true enables installments for this invoice.
        Setting to false will prevent any selected plan from applying to a payment.
        """
        plan: NotRequired[
            "Literal['']|InvoiceService.CreateParamsPaymentSettingsPaymentMethodOptionsCardInstallmentsPlan"
        ]
        """
        The selected installment plan to use for this invoice.
        """

    class CreateParamsPaymentSettingsPaymentMethodOptionsCardInstallmentsPlan(
        TypedDict,
    ):
        count: NotRequired[int]
        """
        For `fixed_count` installment plans, this is required. It represents the number of installment payments your customer will make to their credit card.
        """
        interval: NotRequired[Literal["month"]]
        """
        For `fixed_count` installment plans, this is required. It represents the interval between installment payments your customer will make to their credit card.
        One of `month`.
        """
        type: Literal["fixed_count"]
        """
        Type of installment plan, one of `fixed_count`.
        """

    class CreateParamsPaymentSettingsPaymentMethodOptionsCustomerBalance(
        TypedDict,
    ):
        bank_transfer: NotRequired[
            "InvoiceService.CreateParamsPaymentSettingsPaymentMethodOptionsCustomerBalanceBankTransfer"
        ]
        """
        Configuration for the bank transfer funding type, if the `funding_type` is set to `bank_transfer`.
        """
        funding_type: NotRequired[str]
        """
        The funding method type to be used when there are not enough funds in the customer balance. Permitted values include: `bank_transfer`.
        """

    class CreateParamsPaymentSettingsPaymentMethodOptionsCustomerBalanceBankTransfer(
        TypedDict,
    ):
        eu_bank_transfer: NotRequired[
            "InvoiceService.CreateParamsPaymentSettingsPaymentMethodOptionsCustomerBalanceBankTransferEuBankTransfer"
        ]
        """
        Configuration for eu_bank_transfer funding type.
        """
        type: NotRequired[str]
        """
        The bank transfer type that can be used for funding. Permitted values include: `eu_bank_transfer`, `gb_bank_transfer`, `jp_bank_transfer`, `mx_bank_transfer`, or `us_bank_transfer`.
        """

    class CreateParamsPaymentSettingsPaymentMethodOptionsCustomerBalanceBankTransferEuBankTransfer(
        TypedDict,
    ):
        country: str
        """
        The desired country code of the bank account information. Permitted values include: `BE`, `DE`, `ES`, `FR`, `IE`, or `NL`.
        """

    class CreateParamsPaymentSettingsPaymentMethodOptionsKonbini(TypedDict):
        pass

    class CreateParamsPaymentSettingsPaymentMethodOptionsSepaDebit(TypedDict):
        pass

    class CreateParamsPaymentSettingsPaymentMethodOptionsUsBankAccount(
        TypedDict,
    ):
        financial_connections: NotRequired[
            "InvoiceService.CreateParamsPaymentSettingsPaymentMethodOptionsUsBankAccountFinancialConnections"
        ]
        """
        Additional fields for Financial Connections Session creation
        """
        verification_method: NotRequired[
            Literal["automatic", "instant", "microdeposits"]
        ]
        """
        Verification method for the intent
        """

    class CreateParamsPaymentSettingsPaymentMethodOptionsUsBankAccountFinancialConnections(
        TypedDict,
    ):
        filters: NotRequired[
            "InvoiceService.CreateParamsPaymentSettingsPaymentMethodOptionsUsBankAccountFinancialConnectionsFilters"
        ]
        """
        Provide filters for the linked accounts that the customer can select for the payment method.
        """
        permissions: NotRequired[
            List[
                Literal[
                    "balances", "ownership", "payment_method", "transactions"
                ]
            ]
        ]
        """
        The list of permissions to request. If this parameter is passed, the `payment_method` permission must be included. Valid permissions include: `balances`, `ownership`, `payment_method`, and `transactions`.
        """
        prefetch: NotRequired[
            List[Literal["balances", "ownership", "transactions"]]
        ]
        """
        List of data features that you would like to retrieve upon account creation.
        """

    class CreateParamsPaymentSettingsPaymentMethodOptionsUsBankAccountFinancialConnectionsFilters(
        TypedDict,
    ):
        account_subcategories: NotRequired[
            List[Literal["checking", "savings"]]
        ]
        """
        The account subcategories to use to filter for selectable accounts. Valid subcategories are `checking` and `savings`.
        """

    class CreateParamsRendering(TypedDict):
        amount_tax_display: NotRequired[
            "Literal['']|Literal['exclude_tax', 'include_inclusive_tax']"
        ]
        """
        How line-item prices and amounts will be displayed with respect to tax on invoice PDFs. One of `exclude_tax` or `include_inclusive_tax`. `include_inclusive_tax` will include inclusive tax (and exclude exclusive tax) in invoice PDF amounts. `exclude_tax` will exclude all tax (inclusive and exclusive alike) from invoice PDF amounts.
        """
        pdf: NotRequired["InvoiceService.CreateParamsRenderingPdf"]
        """
        Invoice pdf rendering options
        """

    class CreateParamsRenderingPdf(TypedDict):
        page_size: NotRequired[Literal["a4", "auto", "letter"]]
        """
        Page size for invoice PDF. Can be set to `a4`, `letter`, or `auto`.
         If set to `auto`, invoice PDF page size defaults to `a4` for customers with
         Japanese locale and `letter` for customers with other locales.
        """

    class CreateParamsShippingCost(TypedDict):
        shipping_rate: NotRequired[str]
        """
        The ID of the shipping rate to use for this order.
        """
        shipping_rate_data: NotRequired[
            "InvoiceService.CreateParamsShippingCostShippingRateData"
        ]
        """
        Parameters to create a new ad-hoc shipping rate for this order.
        """

    class CreateParamsShippingCostShippingRateData(TypedDict):
        delivery_estimate: NotRequired[
            "InvoiceService.CreateParamsShippingCostShippingRateDataDeliveryEstimate"
        ]
        """
        The estimated range for how long shipping will take, meant to be displayable to the customer. This will appear on CheckoutSessions.
        """
        display_name: str
        """
        The name of the shipping rate, meant to be displayable to the customer. This will appear on CheckoutSessions.
        """
        fixed_amount: NotRequired[
            "InvoiceService.CreateParamsShippingCostShippingRateDataFixedAmount"
        ]
        """
        Describes a fixed amount to charge for shipping. Must be present if type is `fixed_amount`.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        tax_behavior: NotRequired[
            Literal["exclusive", "inclusive", "unspecified"]
        ]
        """
        Specifies whether the rate is considered inclusive of taxes or exclusive of taxes. One of `inclusive`, `exclusive`, or `unspecified`.
        """
        tax_code: NotRequired[str]
        """
        A [tax code](https://stripe.com/docs/tax/tax-categories) ID. The Shipping tax code is `txcd_92010001`.
        """
        type: NotRequired[Literal["fixed_amount"]]
        """
        The type of calculation to use on the shipping rate.
        """

    class CreateParamsShippingCostShippingRateDataDeliveryEstimate(TypedDict):
        maximum: NotRequired[
            "InvoiceService.CreateParamsShippingCostShippingRateDataDeliveryEstimateMaximum"
        ]
        """
        The upper bound of the estimated range. If empty, represents no upper bound i.e., infinite.
        """
        minimum: NotRequired[
            "InvoiceService.CreateParamsShippingCostShippingRateDataDeliveryEstimateMinimum"
        ]
        """
        The lower bound of the estimated range. If empty, represents no lower bound.
        """

    class CreateParamsShippingCostShippingRateDataDeliveryEstimateMaximum(
        TypedDict,
    ):
        unit: Literal["business_day", "day", "hour", "month", "week"]
        """
        A unit of time.
        """
        value: int
        """
        Must be greater than 0.
        """

    class CreateParamsShippingCostShippingRateDataDeliveryEstimateMinimum(
        TypedDict,
    ):
        unit: Literal["business_day", "day", "hour", "month", "week"]
        """
        A unit of time.
        """
        value: int
        """
        Must be greater than 0.
        """

    class CreateParamsShippingCostShippingRateDataFixedAmount(TypedDict):
        amount: int
        """
        A non-negative integer in cents representing how much to charge.
        """
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        currency_options: NotRequired[
            Dict[
                str,
                "InvoiceService.CreateParamsShippingCostShippingRateDataFixedAmountCurrencyOptions",
            ]
        ]
        """
        Shipping rates defined in each available currency option. Each key must be a three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html) and a [supported currency](https://stripe.com/docs/currencies).
        """

    class CreateParamsShippingCostShippingRateDataFixedAmountCurrencyOptions(
        TypedDict,
    ):
        amount: int
        """
        A non-negative integer in cents representing how much to charge.
        """
        tax_behavior: NotRequired[
            Literal["exclusive", "inclusive", "unspecified"]
        ]
        """
        Specifies whether the rate is considered inclusive of taxes or exclusive of taxes. One of `inclusive`, `exclusive`, or `unspecified`.
        """

    class CreateParamsShippingDetails(TypedDict):
        address: "InvoiceService.CreateParamsShippingDetailsAddress"
        """
        Shipping address
        """
        name: str
        """
        Recipient name.
        """
        phone: NotRequired["Literal['']|str"]
        """
        Recipient phone (including extension)
        """

    class CreateParamsShippingDetailsAddress(TypedDict):
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
        The amount that will be transferred automatically when the invoice is paid. If no amount is set, the full amount is transferred.
        """
        destination: str
        """
        ID of an existing, connected Stripe account.
        """

    class CreatePreviewParams(TypedDict):
        automatic_tax: NotRequired[
            "InvoiceService.CreatePreviewParamsAutomaticTax"
        ]
        """
        Settings for automatic tax lookup for this invoice preview.
        """
        coupon: NotRequired[str]
        """
        The ID of the coupon to apply to this phase of the subscription schedule. This field has been deprecated and will be removed in a future API version. Use `discounts` instead.
        """
        currency: NotRequired[str]
        """
        The currency to preview this invoice in. Defaults to that of `customer` if not specified.
        """
        customer: NotRequired[str]
        """
        The identifier of the customer whose upcoming invoice you'd like to retrieve. If `automatic_tax` is enabled then one of `customer`, `customer_details`, `subscription`, or `schedule` must be set.
        """
        customer_details: NotRequired[
            "InvoiceService.CreatePreviewParamsCustomerDetails"
        ]
        """
        Details about the customer you want to invoice or overrides for an existing customer. If `automatic_tax` is enabled then one of `customer`, `customer_details`, `subscription`, or `schedule` must be set.
        """
        discounts: NotRequired[
            "Literal['']|List[InvoiceService.CreatePreviewParamsDiscount]"
        ]
        """
        The coupons to redeem into discounts for the invoice preview. If not specified, inherits the discount from the subscription or customer. This works for both coupons directly applied to an invoice and coupons applied to a subscription. Pass an empty string to avoid inheriting any discounts.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        invoice_items: NotRequired[
            List["InvoiceService.CreatePreviewParamsInvoiceItem"]
        ]
        """
        List of invoice items to add or update in the upcoming invoice preview.
        """
        issuer: NotRequired["InvoiceService.CreatePreviewParamsIssuer"]
        """
        The connected account that issues the invoice. The invoice is presented with the branding and support information of the specified account.
        """
        on_behalf_of: NotRequired["Literal['']|str"]
        """
        The account (if any) for which the funds of the invoice payment are intended. If set, the invoice will be presented with the branding and support information of the specified account. See the [Invoices with Connect](https://stripe.com/docs/billing/invoices/connect) documentation for details.
        """
        preview_mode: NotRequired[Literal["next", "recurring"]]
        """
        Customizes the types of values to include when calculating the invoice. Defaults to `next` if unspecified.
        """
        schedule: NotRequired[str]
        """
        The identifier of the schedule whose upcoming invoice you'd like to retrieve. Cannot be used with subscription or subscription fields.
        """
        schedule_details: NotRequired[
            "InvoiceService.CreatePreviewParamsScheduleDetails"
        ]
        """
        The schedule creation or modification params to apply as a preview. Cannot be used with `subscription` or `subscription_` prefixed fields.
        """
        subscription: NotRequired[str]
        """
        The identifier of the subscription for which you'd like to retrieve the upcoming invoice. If not provided, but a `subscription_details.items` is provided, you will preview creating a subscription with those items. If neither `subscription` nor `subscription_details.items` is provided, you will retrieve the next upcoming invoice from among the customer's subscriptions.
        """
        subscription_details: NotRequired[
            "InvoiceService.CreatePreviewParamsSubscriptionDetails"
        ]
        """
        The subscription creation or modification params to apply as a preview. Cannot be used with `schedule` or `schedule_details` fields.
        """

    class CreatePreviewParamsAutomaticTax(TypedDict):
        enabled: bool
        """
        Whether Stripe automatically computes tax on this invoice. Note that incompatible invoice items (invoice items with manually specified [tax rates](https://stripe.com/docs/api/tax_rates), negative amounts, or `tax_behavior=unspecified`) cannot be added to automatic tax invoices.
        """
        liability: NotRequired[
            "InvoiceService.CreatePreviewParamsAutomaticTaxLiability"
        ]
        """
        The account that's liable for tax. If set, the business address and tax registrations required to perform the tax calculation are loaded from this account. The tax transaction is returned in the report of the connected account.
        """

    class CreatePreviewParamsAutomaticTaxLiability(TypedDict):
        account: NotRequired[str]
        """
        The connected account being referenced when `type` is `account`.
        """
        type: Literal["account", "self"]
        """
        Type of the account referenced in the request.
        """

    class CreatePreviewParamsCustomerDetails(TypedDict):
        address: NotRequired[
            "Literal['']|InvoiceService.CreatePreviewParamsCustomerDetailsAddress"
        ]
        """
        The customer's address.
        """
        shipping: NotRequired[
            "Literal['']|InvoiceService.CreatePreviewParamsCustomerDetailsShipping"
        ]
        """
        The customer's shipping information. Appears on invoices emailed to this customer.
        """
        tax: NotRequired[
            "InvoiceService.CreatePreviewParamsCustomerDetailsTax"
        ]
        """
        Tax details about the customer.
        """
        tax_exempt: NotRequired[
            "Literal['']|Literal['exempt', 'none', 'reverse']"
        ]
        """
        The customer's tax exemption. One of `none`, `exempt`, or `reverse`.
        """
        tax_ids: NotRequired[
            List["InvoiceService.CreatePreviewParamsCustomerDetailsTaxId"]
        ]
        """
        The customer's tax IDs.
        """

    class CreatePreviewParamsCustomerDetailsAddress(TypedDict):
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

    class CreatePreviewParamsCustomerDetailsShipping(TypedDict):
        address: (
            "InvoiceService.CreatePreviewParamsCustomerDetailsShippingAddress"
        )
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

    class CreatePreviewParamsCustomerDetailsShippingAddress(TypedDict):
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

    class CreatePreviewParamsCustomerDetailsTax(TypedDict):
        ip_address: NotRequired["Literal['']|str"]
        """
        A recent IP address of the customer used for tax reporting and tax location inference. Stripe recommends updating the IP address when a new PaymentMethod is attached or the address field on the customer is updated. We recommend against updating this field more frequently since it could result in unexpected tax location/reporting outcomes.
        """

    class CreatePreviewParamsCustomerDetailsTaxId(TypedDict):
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

    class CreatePreviewParamsDiscount(TypedDict):
        coupon: NotRequired[str]
        """
        ID of the coupon to create a new discount for.
        """
        discount: NotRequired[str]
        """
        ID of an existing discount on the object (or one of its ancestors) to reuse.
        """
        promotion_code: NotRequired[str]
        """
        ID of the promotion code to create a new discount for.
        """

    class CreatePreviewParamsInvoiceItem(TypedDict):
        amount: NotRequired[int]
        """
        The integer amount in cents (or local equivalent) of previewed invoice item.
        """
        currency: NotRequired[str]
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies). Only applicable to new invoice items.
        """
        description: NotRequired[str]
        """
        An arbitrary string which you can attach to the invoice item. The description is displayed in the invoice for easy tracking.
        """
        discountable: NotRequired[bool]
        """
        Explicitly controls whether discounts apply to this invoice item. Defaults to true, except for negative invoice items.
        """
        discounts: NotRequired[
            "Literal['']|List[InvoiceService.CreatePreviewParamsInvoiceItemDiscount]"
        ]
        """
        The coupons to redeem into discounts for the invoice item in the preview.
        """
        invoiceitem: NotRequired[str]
        """
        The ID of the invoice item to update in preview. If not specified, a new invoice item will be added to the preview of the upcoming invoice.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        period: NotRequired[
            "InvoiceService.CreatePreviewParamsInvoiceItemPeriod"
        ]
        """
        The period associated with this invoice item. When set to different values, the period will be rendered on the invoice. If you have [Stripe Revenue Recognition](https://stripe.com/docs/revenue-recognition) enabled, the period will be used to recognize and defer revenue. See the [Revenue Recognition documentation](https://stripe.com/docs/revenue-recognition/methodology/subscriptions-and-invoicing) for details.
        """
        price: NotRequired[str]
        """
        The ID of the price object. One of `price` or `price_data` is required.
        """
        price_data: NotRequired[
            "InvoiceService.CreatePreviewParamsInvoiceItemPriceData"
        ]
        """
        Data used to generate a new [Price](https://stripe.com/docs/api/prices) object inline. One of `price` or `price_data` is required.
        """
        quantity: NotRequired[int]
        """
        Non-negative integer. The quantity of units for the invoice item.
        """
        tax_behavior: NotRequired[
            Literal["exclusive", "inclusive", "unspecified"]
        ]
        """
        Only required if a [default tax behavior](https://stripe.com/docs/tax/products-prices-tax-categories-tax-behavior#setting-a-default-tax-behavior-(recommended)) was not provided in the Stripe Tax settings. Specifies whether the price is considered inclusive of taxes or exclusive of taxes. One of `inclusive`, `exclusive`, or `unspecified`. Once specified as either `inclusive` or `exclusive`, it cannot be changed.
        """
        tax_code: NotRequired["Literal['']|str"]
        """
        A [tax code](https://stripe.com/docs/tax/tax-categories) ID.
        """
        tax_rates: NotRequired["Literal['']|List[str]"]
        """
        The tax rates that apply to the item. When set, any `default_tax_rates` do not apply to this item.
        """
        unit_amount: NotRequired[int]
        """
        The integer unit amount in cents (or local equivalent) of the charge to be applied to the upcoming invoice. This unit_amount will be multiplied by the quantity to get the full amount. If you want to apply a credit to the customer's account, pass a negative unit_amount.
        """
        unit_amount_decimal: NotRequired[str]
        """
        Same as `unit_amount`, but accepts a decimal value in cents (or local equivalent) with at most 12 decimal places. Only one of `unit_amount` and `unit_amount_decimal` can be set.
        """

    class CreatePreviewParamsInvoiceItemDiscount(TypedDict):
        coupon: NotRequired[str]
        """
        ID of the coupon to create a new discount for.
        """
        discount: NotRequired[str]
        """
        ID of an existing discount on the object (or one of its ancestors) to reuse.
        """
        promotion_code: NotRequired[str]
        """
        ID of the promotion code to create a new discount for.
        """

    class CreatePreviewParamsInvoiceItemPeriod(TypedDict):
        end: int
        """
        The end of the period, which must be greater than or equal to the start. This value is inclusive.
        """
        start: int
        """
        The start of the period. This value is inclusive.
        """

    class CreatePreviewParamsInvoiceItemPriceData(TypedDict):
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        product: str
        """
        The ID of the product that this price will belong to.
        """
        tax_behavior: NotRequired[
            Literal["exclusive", "inclusive", "unspecified"]
        ]
        """
        Only required if a [default tax behavior](https://stripe.com/docs/tax/products-prices-tax-categories-tax-behavior#setting-a-default-tax-behavior-(recommended)) was not provided in the Stripe Tax settings. Specifies whether the price is considered inclusive of taxes or exclusive of taxes. One of `inclusive`, `exclusive`, or `unspecified`. Once specified as either `inclusive` or `exclusive`, it cannot be changed.
        """
        unit_amount: NotRequired[int]
        """
        A positive integer in cents (or local equivalent) (or 0 for a free price) representing how much to charge.
        """
        unit_amount_decimal: NotRequired[str]
        """
        Same as `unit_amount`, but accepts a decimal value in cents (or local equivalent) with at most 12 decimal places. Only one of `unit_amount` and `unit_amount_decimal` can be set.
        """

    class CreatePreviewParamsIssuer(TypedDict):
        account: NotRequired[str]
        """
        The connected account being referenced when `type` is `account`.
        """
        type: Literal["account", "self"]
        """
        Type of the account referenced in the request.
        """

    class CreatePreviewParamsScheduleDetails(TypedDict):
        end_behavior: NotRequired[Literal["cancel", "release"]]
        """
        Behavior of the subscription schedule and underlying subscription when it ends. Possible values are `release` or `cancel` with the default being `release`. `release` will end the subscription schedule and keep the underlying subscription running. `cancel` will end the subscription schedule and cancel the underlying subscription.
        """
        phases: NotRequired[
            List["InvoiceService.CreatePreviewParamsScheduleDetailsPhase"]
        ]
        """
        List representing phases of the subscription schedule. Each phase can be customized to have different durations, plans, and coupons. If there are multiple phases, the `end_date` of one phase will always equal the `start_date` of the next phase.
        """
        proration_behavior: NotRequired[
            Literal["always_invoice", "create_prorations", "none"]
        ]
        """
        In cases where the `schedule_details` params update the currently active phase, specifies if and how to prorate at the time of the request.
        """

    class CreatePreviewParamsScheduleDetailsPhase(TypedDict):
        add_invoice_items: NotRequired[
            List[
                "InvoiceService.CreatePreviewParamsScheduleDetailsPhaseAddInvoiceItem"
            ]
        ]
        """
        A list of prices and quantities that will generate invoice items appended to the next invoice for this phase. You may pass up to 20 items.
        """
        application_fee_percent: NotRequired[float]
        """
        A non-negative decimal between 0 and 100, with at most two decimal places. This represents the percentage of the subscription invoice total that will be transferred to the application owner's Stripe account. The request must be made by a platform account on a connected account in order to set an application fee percentage. For more information, see the application fees [documentation](https://stripe.com/docs/connect/subscriptions#collecting-fees-on-subscriptions).
        """
        automatic_tax: NotRequired[
            "InvoiceService.CreatePreviewParamsScheduleDetailsPhaseAutomaticTax"
        ]
        """
        Automatic tax settings for this phase.
        """
        billing_cycle_anchor: NotRequired[Literal["automatic", "phase_start"]]
        """
        Can be set to `phase_start` to set the anchor to the start of the phase or `automatic` to automatically change it if needed. Cannot be set to `phase_start` if this phase specifies a trial. For more information, see the billing cycle [documentation](https://stripe.com/docs/billing/subscriptions/billing-cycle).
        """
        billing_thresholds: NotRequired[
            "Literal['']|InvoiceService.CreatePreviewParamsScheduleDetailsPhaseBillingThresholds"
        ]
        """
        Define thresholds at which an invoice will be sent, and the subscription advanced to a new billing period. Pass an empty string to remove previously-defined thresholds.
        """
        collection_method: NotRequired[
            Literal["charge_automatically", "send_invoice"]
        ]
        """
        Either `charge_automatically`, or `send_invoice`. When charging automatically, Stripe will attempt to pay the underlying subscription at the end of each billing cycle using the default source attached to the customer. When sending an invoice, Stripe will email your customer an invoice with payment instructions and mark the subscription as `active`. Defaults to `charge_automatically` on creation.
        """
        coupon: NotRequired[str]
        """
        The ID of the coupon to apply to this phase of the subscription schedule. This field has been deprecated and will be removed in a future API version. Use `discounts` instead.
        """
        currency: NotRequired[str]
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        default_payment_method: NotRequired[str]
        """
        ID of the default payment method for the subscription schedule. It must belong to the customer associated with the subscription schedule. If not set, invoices will use the default payment method in the customer's invoice settings.
        """
        default_tax_rates: NotRequired["Literal['']|List[str]"]
        """
        A list of [Tax Rate](https://stripe.com/docs/api/tax_rates) ids. These Tax Rates will set the Subscription's [`default_tax_rates`](https://stripe.com/docs/api/subscriptions/create#create_subscription-default_tax_rates), which means they will be the Invoice's [`default_tax_rates`](https://stripe.com/docs/api/invoices/create#create_invoice-default_tax_rates) for any Invoices issued by the Subscription during this Phase.
        """
        description: NotRequired["Literal['']|str"]
        """
        Subscription description, meant to be displayable to the customer. Use this field to optionally store an explanation of the subscription for rendering in Stripe surfaces and certain local payment methods UIs.
        """
        discounts: NotRequired[
            "Literal['']|List[InvoiceService.CreatePreviewParamsScheduleDetailsPhaseDiscount]"
        ]
        """
        The coupons to redeem into discounts for the schedule phase. If not specified, inherits the discount from the subscription's customer. Pass an empty string to avoid inheriting any discounts.
        """
        end_date: NotRequired["int|Literal['now']"]
        """
        The date at which this phase of the subscription schedule ends. If set, `iterations` must not be set.
        """
        invoice_settings: NotRequired[
            "InvoiceService.CreatePreviewParamsScheduleDetailsPhaseInvoiceSettings"
        ]
        """
        All invoices will be billed using the specified settings.
        """
        items: List[
            "InvoiceService.CreatePreviewParamsScheduleDetailsPhaseItem"
        ]
        """
        List of configuration items, each with an attached price, to apply during this phase of the subscription schedule.
        """
        iterations: NotRequired[int]
        """
        Integer representing the multiplier applied to the price interval. For example, `iterations=2` applied to a price with `interval=month` and `interval_count=3` results in a phase of duration `2 * 3 months = 6 months`. If set, `end_date` must not be set.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to a phase. Metadata on a schedule's phase will update the underlying subscription's `metadata` when the phase is entered, adding new keys and replacing existing keys in the subscription's `metadata`. Individual keys in the subscription's `metadata` can be unset by posting an empty value to them in the phase's `metadata`. To unset all keys in the subscription's `metadata`, update the subscription directly or unset every key individually from the phase's `metadata`.
        """
        on_behalf_of: NotRequired[str]
        """
        The account on behalf of which to charge, for each of the associated subscription's invoices.
        """
        proration_behavior: NotRequired[
            Literal["always_invoice", "create_prorations", "none"]
        ]
        """
        Whether the subscription schedule will create [prorations](https://stripe.com/docs/billing/subscriptions/prorations) when transitioning to this phase. The default value is `create_prorations`. This setting controls prorations when a phase is started asynchronously and it is persisted as a field on the phase. It's different from the request-level [proration_behavior](https://stripe.com/docs/api/subscription_schedules/update#update_subscription_schedule-proration_behavior) parameter which controls what happens if the update request affects the billing configuration of the current phase.
        """
        start_date: NotRequired["int|Literal['now']"]
        """
        The date at which this phase of the subscription schedule starts or `now`. Must be set on the first phase.
        """
        transfer_data: NotRequired[
            "InvoiceService.CreatePreviewParamsScheduleDetailsPhaseTransferData"
        ]
        """
        The data with which to automatically create a Transfer for each of the associated subscription's invoices.
        """
        trial: NotRequired[bool]
        """
        If set to true the entire phase is counted as a trial and the customer will not be charged for any fees.
        """
        trial_end: NotRequired["int|Literal['now']"]
        """
        Sets the phase to trialing from the start date to this date. Must be before the phase end date, can not be combined with `trial`
        """

    class CreatePreviewParamsScheduleDetailsPhaseAddInvoiceItem(TypedDict):
        discounts: NotRequired[
            List[
                "InvoiceService.CreatePreviewParamsScheduleDetailsPhaseAddInvoiceItemDiscount"
            ]
        ]
        """
        The coupons to redeem into discounts for the item.
        """
        price: NotRequired[str]
        """
        The ID of the price object. One of `price` or `price_data` is required.
        """
        price_data: NotRequired[
            "InvoiceService.CreatePreviewParamsScheduleDetailsPhaseAddInvoiceItemPriceData"
        ]
        """
        Data used to generate a new [Price](https://stripe.com/docs/api/prices) object inline. One of `price` or `price_data` is required.
        """
        quantity: NotRequired[int]
        """
        Quantity for this item. Defaults to 1.
        """
        tax_rates: NotRequired["Literal['']|List[str]"]
        """
        The tax rates which apply to the item. When set, the `default_tax_rates` do not apply to this item.
        """

    class CreatePreviewParamsScheduleDetailsPhaseAddInvoiceItemDiscount(
        TypedDict,
    ):
        coupon: NotRequired[str]
        """
        ID of the coupon to create a new discount for.
        """
        discount: NotRequired[str]
        """
        ID of an existing discount on the object (or one of its ancestors) to reuse.
        """
        promotion_code: NotRequired[str]
        """
        ID of the promotion code to create a new discount for.
        """

    class CreatePreviewParamsScheduleDetailsPhaseAddInvoiceItemPriceData(
        TypedDict,
    ):
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        product: str
        """
        The ID of the product that this price will belong to.
        """
        tax_behavior: NotRequired[
            Literal["exclusive", "inclusive", "unspecified"]
        ]
        """
        Only required if a [default tax behavior](https://stripe.com/docs/tax/products-prices-tax-categories-tax-behavior#setting-a-default-tax-behavior-(recommended)) was not provided in the Stripe Tax settings. Specifies whether the price is considered inclusive of taxes or exclusive of taxes. One of `inclusive`, `exclusive`, or `unspecified`. Once specified as either `inclusive` or `exclusive`, it cannot be changed.
        """
        unit_amount: NotRequired[int]
        """
        A positive integer in cents (or local equivalent) (or 0 for a free price) representing how much to charge.
        """
        unit_amount_decimal: NotRequired[str]
        """
        Same as `unit_amount`, but accepts a decimal value in cents (or local equivalent) with at most 12 decimal places. Only one of `unit_amount` and `unit_amount_decimal` can be set.
        """

    class CreatePreviewParamsScheduleDetailsPhaseAutomaticTax(TypedDict):
        enabled: bool
        """
        Enabled automatic tax calculation which will automatically compute tax rates on all invoices generated by the subscription.
        """
        liability: NotRequired[
            "InvoiceService.CreatePreviewParamsScheduleDetailsPhaseAutomaticTaxLiability"
        ]
        """
        The account that's liable for tax. If set, the business address and tax registrations required to perform the tax calculation are loaded from this account. The tax transaction is returned in the report of the connected account.
        """

    class CreatePreviewParamsScheduleDetailsPhaseAutomaticTaxLiability(
        TypedDict,
    ):
        account: NotRequired[str]
        """
        The connected account being referenced when `type` is `account`.
        """
        type: Literal["account", "self"]
        """
        Type of the account referenced in the request.
        """

    class CreatePreviewParamsScheduleDetailsPhaseBillingThresholds(TypedDict):
        amount_gte: NotRequired[int]
        """
        Monetary threshold that triggers the subscription to advance to a new billing period
        """
        reset_billing_cycle_anchor: NotRequired[bool]
        """
        Indicates if the `billing_cycle_anchor` should be reset when a threshold is reached. If true, `billing_cycle_anchor` will be updated to the date/time the threshold was last reached; otherwise, the value will remain unchanged.
        """

    class CreatePreviewParamsScheduleDetailsPhaseDiscount(TypedDict):
        coupon: NotRequired[str]
        """
        ID of the coupon to create a new discount for.
        """
        discount: NotRequired[str]
        """
        ID of an existing discount on the object (or one of its ancestors) to reuse.
        """
        promotion_code: NotRequired[str]
        """
        ID of the promotion code to create a new discount for.
        """

    class CreatePreviewParamsScheduleDetailsPhaseInvoiceSettings(TypedDict):
        account_tax_ids: NotRequired["Literal['']|List[str]"]
        """
        The account tax IDs associated with this phase of the subscription schedule. Will be set on invoices generated by this phase of the subscription schedule.
        """
        days_until_due: NotRequired[int]
        """
        Number of days within which a customer must pay invoices generated by this subscription schedule. This value will be `null` for subscription schedules where `billing=charge_automatically`.
        """
        issuer: NotRequired[
            "InvoiceService.CreatePreviewParamsScheduleDetailsPhaseInvoiceSettingsIssuer"
        ]
        """
        The connected account that issues the invoice. The invoice is presented with the branding and support information of the specified account.
        """

    class CreatePreviewParamsScheduleDetailsPhaseInvoiceSettingsIssuer(
        TypedDict,
    ):
        account: NotRequired[str]
        """
        The connected account being referenced when `type` is `account`.
        """
        type: Literal["account", "self"]
        """
        Type of the account referenced in the request.
        """

    class CreatePreviewParamsScheduleDetailsPhaseItem(TypedDict):
        billing_thresholds: NotRequired[
            "Literal['']|InvoiceService.CreatePreviewParamsScheduleDetailsPhaseItemBillingThresholds"
        ]
        """
        Define thresholds at which an invoice will be sent, and the subscription advanced to a new billing period. When updating, pass an empty string to remove previously-defined thresholds.
        """
        discounts: NotRequired[
            "Literal['']|List[InvoiceService.CreatePreviewParamsScheduleDetailsPhaseItemDiscount]"
        ]
        """
        The coupons to redeem into discounts for the subscription item.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to a configuration item. Metadata on a configuration item will update the underlying subscription item's `metadata` when the phase is entered, adding new keys and replacing existing keys. Individual keys in the subscription item's `metadata` can be unset by posting an empty value to them in the configuration item's `metadata`. To unset all keys in the subscription item's `metadata`, update the subscription item directly or unset every key individually from the configuration item's `metadata`.
        """
        plan: NotRequired[str]
        """
        The plan ID to subscribe to. You may specify the same ID in `plan` and `price`.
        """
        price: NotRequired[str]
        """
        The ID of the price object.
        """
        price_data: NotRequired[
            "InvoiceService.CreatePreviewParamsScheduleDetailsPhaseItemPriceData"
        ]
        """
        Data used to generate a new [Price](https://stripe.com/docs/api/prices) object inline.
        """
        quantity: NotRequired[int]
        """
        Quantity for the given price. Can be set only if the price's `usage_type` is `licensed` and not `metered`.
        """
        tax_rates: NotRequired["Literal['']|List[str]"]
        """
        A list of [Tax Rate](https://stripe.com/docs/api/tax_rates) ids. These Tax Rates will override the [`default_tax_rates`](https://stripe.com/docs/api/subscriptions/create#create_subscription-default_tax_rates) on the Subscription. When updating, pass an empty string to remove previously-defined tax rates.
        """

    class CreatePreviewParamsScheduleDetailsPhaseItemBillingThresholds(
        TypedDict,
    ):
        usage_gte: int
        """
        Number of units that meets the billing threshold to advance the subscription to a new billing period (e.g., it takes 10 $5 units to meet a $50 [monetary threshold](https://stripe.com/docs/api/subscriptions/update#update_subscription-billing_thresholds-amount_gte))
        """

    class CreatePreviewParamsScheduleDetailsPhaseItemDiscount(TypedDict):
        coupon: NotRequired[str]
        """
        ID of the coupon to create a new discount for.
        """
        discount: NotRequired[str]
        """
        ID of an existing discount on the object (or one of its ancestors) to reuse.
        """
        promotion_code: NotRequired[str]
        """
        ID of the promotion code to create a new discount for.
        """

    class CreatePreviewParamsScheduleDetailsPhaseItemPriceData(TypedDict):
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        product: str
        """
        The ID of the product that this price will belong to.
        """
        recurring: "InvoiceService.CreatePreviewParamsScheduleDetailsPhaseItemPriceDataRecurring"
        """
        The recurring components of a price such as `interval` and `interval_count`.
        """
        tax_behavior: NotRequired[
            Literal["exclusive", "inclusive", "unspecified"]
        ]
        """
        Only required if a [default tax behavior](https://stripe.com/docs/tax/products-prices-tax-categories-tax-behavior#setting-a-default-tax-behavior-(recommended)) was not provided in the Stripe Tax settings. Specifies whether the price is considered inclusive of taxes or exclusive of taxes. One of `inclusive`, `exclusive`, or `unspecified`. Once specified as either `inclusive` or `exclusive`, it cannot be changed.
        """
        unit_amount: NotRequired[int]
        """
        A positive integer in cents (or local equivalent) (or 0 for a free price) representing how much to charge.
        """
        unit_amount_decimal: NotRequired[str]
        """
        Same as `unit_amount`, but accepts a decimal value in cents (or local equivalent) with at most 12 decimal places. Only one of `unit_amount` and `unit_amount_decimal` can be set.
        """

    class CreatePreviewParamsScheduleDetailsPhaseItemPriceDataRecurring(
        TypedDict,
    ):
        interval: Literal["day", "month", "week", "year"]
        """
        Specifies billing frequency. Either `day`, `week`, `month` or `year`.
        """
        interval_count: NotRequired[int]
        """
        The number of intervals between subscription billings. For example, `interval=month` and `interval_count=3` bills every 3 months. Maximum of three years interval allowed (3 years, 36 months, or 156 weeks).
        """

    class CreatePreviewParamsScheduleDetailsPhaseTransferData(TypedDict):
        amount_percent: NotRequired[float]
        """
        A non-negative decimal between 0 and 100, with at most two decimal places. This represents the percentage of the subscription invoice total that will be transferred to the destination account. By default, the entire amount is transferred to the destination.
        """
        destination: str
        """
        ID of an existing, connected Stripe account.
        """

    class CreatePreviewParamsSubscriptionDetails(TypedDict):
        billing_cycle_anchor: NotRequired["Literal['now', 'unchanged']|int"]
        """
        For new subscriptions, a future timestamp to anchor the subscription's [billing cycle](https://stripe.com/docs/subscriptions/billing-cycle). This is used to determine the date of the first full invoice, and, for plans with `month` or `year` intervals, the day of the month for subsequent invoices. For existing subscriptions, the value can only be set to `now` or `unchanged`.
        """
        cancel_at: NotRequired["Literal['']|int"]
        """
        A timestamp at which the subscription should cancel. If set to a date before the current period ends, this will cause a proration if prorations have been enabled using `proration_behavior`. If set during a future period, this will always cause a proration for that period.
        """
        cancel_at_period_end: NotRequired[bool]
        """
        Indicate whether this subscription should cancel at the end of the current period (`current_period_end`). Defaults to `false`.
        """
        cancel_now: NotRequired[bool]
        """
        This simulates the subscription being canceled or expired immediately.
        """
        default_tax_rates: NotRequired["Literal['']|List[str]"]
        """
        If provided, the invoice returned will preview updating or creating a subscription with these default tax rates. The default tax rates will apply to any line item that does not have `tax_rates` set.
        """
        items: NotRequired[
            List["InvoiceService.CreatePreviewParamsSubscriptionDetailsItem"]
        ]
        """
        A list of up to 20 subscription items, each with an attached price.
        """
        proration_behavior: NotRequired[
            Literal["always_invoice", "create_prorations", "none"]
        ]
        """
        Determines how to handle [prorations](https://stripe.com/docs/billing/subscriptions/prorations) when the billing cycle changes (e.g., when switching plans, resetting `billing_cycle_anchor=now`, or starting a trial), or if an item's `quantity` changes. The default value is `create_prorations`.
        """
        proration_date: NotRequired[int]
        """
        If previewing an update to a subscription, and doing proration, `subscription_details.proration_date` forces the proration to be calculated as though the update was done at the specified time. The time given must be within the current subscription period and within the current phase of the schedule backing this subscription, if the schedule exists. If set, `subscription`, and one of `subscription_details.items`, or `subscription_details.trial_end` are required. Also, `subscription_details.proration_behavior` cannot be set to 'none'.
        """
        resume_at: NotRequired[Literal["now"]]
        """
        For paused subscriptions, setting `subscription_details.resume_at` to `now` will preview the invoice that will be generated if the subscription is resumed.
        """
        start_date: NotRequired[int]
        """
        Date a subscription is intended to start (can be future or past).
        """
        trial_end: NotRequired["Literal['now']|int"]
        """
        If provided, the invoice returned will preview updating or creating a subscription with that trial end. If set, one of `subscription_details.items` or `subscription` is required.
        """

    class CreatePreviewParamsSubscriptionDetailsItem(TypedDict):
        billing_thresholds: NotRequired[
            "Literal['']|InvoiceService.CreatePreviewParamsSubscriptionDetailsItemBillingThresholds"
        ]
        """
        Define thresholds at which an invoice will be sent, and the subscription advanced to a new billing period. When updating, pass an empty string to remove previously-defined thresholds.
        """
        clear_usage: NotRequired[bool]
        """
        Delete all usage for a given subscription item. Allowed only when `deleted` is set to `true` and the current plan's `usage_type` is `metered`.
        """
        deleted: NotRequired[bool]
        """
        A flag that, if set to `true`, will delete the specified item.
        """
        discounts: NotRequired[
            "Literal['']|List[InvoiceService.CreatePreviewParamsSubscriptionDetailsItemDiscount]"
        ]
        """
        The coupons to redeem into discounts for the subscription item.
        """
        id: NotRequired[str]
        """
        Subscription item to update.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        plan: NotRequired[str]
        """
        Plan ID for this item, as a string.
        """
        price: NotRequired[str]
        """
        The ID of the price object. One of `price` or `price_data` is required. When changing a subscription item's price, `quantity` is set to 1 unless a `quantity` parameter is provided.
        """
        price_data: NotRequired[
            "InvoiceService.CreatePreviewParamsSubscriptionDetailsItemPriceData"
        ]
        """
        Data used to generate a new [Price](https://stripe.com/docs/api/prices) object inline. One of `price` or `price_data` is required.
        """
        quantity: NotRequired[int]
        """
        Quantity for this item.
        """
        tax_rates: NotRequired["Literal['']|List[str]"]
        """
        A list of [Tax Rate](https://stripe.com/docs/api/tax_rates) ids. These Tax Rates will override the [`default_tax_rates`](https://stripe.com/docs/api/subscriptions/create#create_subscription-default_tax_rates) on the Subscription. When updating, pass an empty string to remove previously-defined tax rates.
        """

    class CreatePreviewParamsSubscriptionDetailsItemBillingThresholds(
        TypedDict,
    ):
        usage_gte: int
        """
        Number of units that meets the billing threshold to advance the subscription to a new billing period (e.g., it takes 10 $5 units to meet a $50 [monetary threshold](https://stripe.com/docs/api/subscriptions/update#update_subscription-billing_thresholds-amount_gte))
        """

    class CreatePreviewParamsSubscriptionDetailsItemDiscount(TypedDict):
        coupon: NotRequired[str]
        """
        ID of the coupon to create a new discount for.
        """
        discount: NotRequired[str]
        """
        ID of an existing discount on the object (or one of its ancestors) to reuse.
        """
        promotion_code: NotRequired[str]
        """
        ID of the promotion code to create a new discount for.
        """

    class CreatePreviewParamsSubscriptionDetailsItemPriceData(TypedDict):
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        product: str
        """
        The ID of the product that this price will belong to.
        """
        recurring: "InvoiceService.CreatePreviewParamsSubscriptionDetailsItemPriceDataRecurring"
        """
        The recurring components of a price such as `interval` and `interval_count`.
        """
        tax_behavior: NotRequired[
            Literal["exclusive", "inclusive", "unspecified"]
        ]
        """
        Only required if a [default tax behavior](https://stripe.com/docs/tax/products-prices-tax-categories-tax-behavior#setting-a-default-tax-behavior-(recommended)) was not provided in the Stripe Tax settings. Specifies whether the price is considered inclusive of taxes or exclusive of taxes. One of `inclusive`, `exclusive`, or `unspecified`. Once specified as either `inclusive` or `exclusive`, it cannot be changed.
        """
        unit_amount: NotRequired[int]
        """
        A positive integer in cents (or local equivalent) (or 0 for a free price) representing how much to charge.
        """
        unit_amount_decimal: NotRequired[str]
        """
        Same as `unit_amount`, but accepts a decimal value in cents (or local equivalent) with at most 12 decimal places. Only one of `unit_amount` and `unit_amount_decimal` can be set.
        """

    class CreatePreviewParamsSubscriptionDetailsItemPriceDataRecurring(
        TypedDict,
    ):
        interval: Literal["day", "month", "week", "year"]
        """
        Specifies billing frequency. Either `day`, `week`, `month` or `year`.
        """
        interval_count: NotRequired[int]
        """
        The number of intervals between subscription billings. For example, `interval=month` and `interval_count=3` bills every 3 months. Maximum of three years interval allowed (3 years, 36 months, or 156 weeks).
        """

    class DeleteParams(TypedDict):
        pass

    class FinalizeInvoiceParams(TypedDict):
        auto_advance: NotRequired[bool]
        """
        Controls whether Stripe performs [automatic collection](https://stripe.com/docs/invoicing/integration/automatic-advancement-collection) of the invoice. If `false`, the invoice's state doesn't automatically advance without an explicit action.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class ListParams(TypedDict):
        collection_method: NotRequired[
            Literal["charge_automatically", "send_invoice"]
        ]
        """
        The collection method of the invoice to retrieve. Either `charge_automatically` or `send_invoice`.
        """
        created: NotRequired["InvoiceService.ListParamsCreated|int"]
        """
        Only return invoices that were created during the given date interval.
        """
        customer: NotRequired[str]
        """
        Only return invoices for the customer specified by this customer ID.
        """
        due_date: NotRequired["InvoiceService.ListParamsDueDate|int"]
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
        status: NotRequired[
            Literal["draft", "open", "paid", "uncollectible", "void"]
        ]
        """
        The status of the invoice, one of `draft`, `open`, `paid`, `uncollectible`, or `void`. [Learn more](https://stripe.com/docs/billing/invoices/workflow#workflow-overview)
        """
        subscription: NotRequired[str]
        """
        Only return invoices for the subscription specified by this subscription ID.
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

    class ListParamsDueDate(TypedDict):
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

    class MarkUncollectibleParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class PayParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        forgive: NotRequired[bool]
        """
        In cases where the source used to pay the invoice has insufficient funds, passing `forgive=true` controls whether a charge should be attempted for the full amount available on the source, up to the amount to fully pay the invoice. This effectively forgives the difference between the amount available on the source and the amount due.

        Passing `forgive=false` will fail the charge if the source hasn't been pre-funded with the right amount. An example for this case is with ACH Credit Transfers and wires: if the amount wired is less than the amount due by a small amount, you might want to forgive the difference. Defaults to `false`.
        """
        mandate: NotRequired["Literal['']|str"]
        """
        ID of the mandate to be used for this invoice. It must correspond to the payment method used to pay the invoice, including the payment_method param or the invoice's default_payment_method or default_source, if set.
        """
        off_session: NotRequired[bool]
        """
        Indicates if a customer is on or off-session while an invoice payment is attempted. Defaults to `true` (off-session).
        """
        paid_out_of_band: NotRequired[bool]
        """
        Boolean representing whether an invoice is paid outside of Stripe. This will result in no charge being made. Defaults to `false`.
        """
        payment_method: NotRequired[str]
        """
        A PaymentMethod to be charged. The PaymentMethod must be the ID of a PaymentMethod belonging to the customer associated with the invoice being paid.
        """
        source: NotRequired[str]
        """
        A payment source to be charged. The source must be the ID of a source belonging to the customer associated with the invoice being paid.
        """

    class RemoveLinesParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        invoice_metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        lines: List["InvoiceService.RemoveLinesParamsLine"]
        """
        The line items to remove.
        """

    class RemoveLinesParamsLine(TypedDict):
        behavior: Literal["delete", "unassign"]
        """
        Either `delete` or `unassign`. Deleted line items are permanently deleted. Unassigned line items can be reassigned to an invoice.
        """
        id: str
        """
        ID of an existing line item to remove from this invoice.
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
        The search query string. See [search query language](https://stripe.com/docs/search#search-query-language) and the list of supported [query fields for invoices](https://stripe.com/docs/search#query-fields-for-invoices).
        """

    class SendInvoiceParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class UpcomingParams(TypedDict):
        automatic_tax: NotRequired["InvoiceService.UpcomingParamsAutomaticTax"]
        """
        Settings for automatic tax lookup for this invoice preview.
        """
        coupon: NotRequired[str]
        """
        The ID of the coupon to apply to this phase of the subscription schedule. This field has been deprecated and will be removed in a future API version. Use `discounts` instead.
        """
        currency: NotRequired[str]
        """
        The currency to preview this invoice in. Defaults to that of `customer` if not specified.
        """
        customer: NotRequired[str]
        """
        The identifier of the customer whose upcoming invoice you'd like to retrieve. If `automatic_tax` is enabled then one of `customer`, `customer_details`, `subscription`, or `schedule` must be set.
        """
        customer_details: NotRequired[
            "InvoiceService.UpcomingParamsCustomerDetails"
        ]
        """
        Details about the customer you want to invoice or overrides for an existing customer. If `automatic_tax` is enabled then one of `customer`, `customer_details`, `subscription`, or `schedule` must be set.
        """
        discounts: NotRequired[
            "Literal['']|List[InvoiceService.UpcomingParamsDiscount]"
        ]
        """
        The coupons to redeem into discounts for the invoice preview. If not specified, inherits the discount from the subscription or customer. This works for both coupons directly applied to an invoice and coupons applied to a subscription. Pass an empty string to avoid inheriting any discounts.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        invoice_items: NotRequired[
            List["InvoiceService.UpcomingParamsInvoiceItem"]
        ]
        """
        List of invoice items to add or update in the upcoming invoice preview.
        """
        issuer: NotRequired["InvoiceService.UpcomingParamsIssuer"]
        """
        The connected account that issues the invoice. The invoice is presented with the branding and support information of the specified account.
        """
        on_behalf_of: NotRequired["Literal['']|str"]
        """
        The account (if any) for which the funds of the invoice payment are intended. If set, the invoice will be presented with the branding and support information of the specified account. See the [Invoices with Connect](https://stripe.com/docs/billing/invoices/connect) documentation for details.
        """
        preview_mode: NotRequired[Literal["next", "recurring"]]
        """
        Customizes the types of values to include when calculating the invoice. Defaults to `next` if unspecified.
        """
        schedule: NotRequired[str]
        """
        The identifier of the schedule whose upcoming invoice you'd like to retrieve. Cannot be used with subscription or subscription fields.
        """
        schedule_details: NotRequired[
            "InvoiceService.UpcomingParamsScheduleDetails"
        ]
        """
        The schedule creation or modification params to apply as a preview. Cannot be used with `subscription` or `subscription_` prefixed fields.
        """
        subscription: NotRequired[str]
        """
        The identifier of the subscription for which you'd like to retrieve the upcoming invoice. If not provided, but a `subscription_details.items` is provided, you will preview creating a subscription with those items. If neither `subscription` nor `subscription_details.items` is provided, you will retrieve the next upcoming invoice from among the customer's subscriptions.
        """
        subscription_billing_cycle_anchor: NotRequired[
            "Literal['now', 'unchanged']|int"
        ]
        """
        For new subscriptions, a future timestamp to anchor the subscription's [billing cycle](https://stripe.com/docs/subscriptions/billing-cycle). This is used to determine the date of the first full invoice, and, for plans with `month` or `year` intervals, the day of the month for subsequent invoices. For existing subscriptions, the value can only be set to `now` or `unchanged`. This field has been deprecated and will be removed in a future API version. Use `subscription_details.billing_cycle_anchor` instead.
        """
        subscription_cancel_at: NotRequired["Literal['']|int"]
        """
        A timestamp at which the subscription should cancel. If set to a date before the current period ends, this will cause a proration if prorations have been enabled using `proration_behavior`. If set during a future period, this will always cause a proration for that period. This field has been deprecated and will be removed in a future API version. Use `subscription_details.cancel_at` instead.
        """
        subscription_cancel_at_period_end: NotRequired[bool]
        """
        Indicate whether this subscription should cancel at the end of the current period (`current_period_end`). Defaults to `false`. This field has been deprecated and will be removed in a future API version. Use `subscription_details.cancel_at_period_end` instead.
        """
        subscription_cancel_now: NotRequired[bool]
        """
        This simulates the subscription being canceled or expired immediately. This field has been deprecated and will be removed in a future API version. Use `subscription_details.cancel_now` instead.
        """
        subscription_default_tax_rates: NotRequired["Literal['']|List[str]"]
        """
        If provided, the invoice returned will preview updating or creating a subscription with these default tax rates. The default tax rates will apply to any line item that does not have `tax_rates` set. This field has been deprecated and will be removed in a future API version. Use `subscription_details.default_tax_rates` instead.
        """
        subscription_details: NotRequired[
            "InvoiceService.UpcomingParamsSubscriptionDetails"
        ]
        """
        The subscription creation or modification params to apply as a preview. Cannot be used with `schedule` or `schedule_details` fields.
        """
        subscription_items: NotRequired[
            List["InvoiceService.UpcomingParamsSubscriptionItem"]
        ]
        """
        A list of up to 20 subscription items, each with an attached price. This field has been deprecated and will be removed in a future API version. Use `subscription_details.items` instead.
        """
        subscription_proration_behavior: NotRequired[
            Literal["always_invoice", "create_prorations", "none"]
        ]
        """
        Determines how to handle [prorations](https://stripe.com/docs/billing/subscriptions/prorations) when the billing cycle changes (e.g., when switching plans, resetting `billing_cycle_anchor=now`, or starting a trial), or if an item's `quantity` changes. The default value is `create_prorations`. This field has been deprecated and will be removed in a future API version. Use `subscription_details.proration_behavior` instead.
        """
        subscription_proration_date: NotRequired[int]
        """
        If previewing an update to a subscription, and doing proration, `subscription_proration_date` forces the proration to be calculated as though the update was done at the specified time. The time given must be within the current subscription period and within the current phase of the schedule backing this subscription, if the schedule exists. If set, `subscription`, and one of `subscription_items`, or `subscription_trial_end` are required. Also, `subscription_proration_behavior` cannot be set to 'none'. This field has been deprecated and will be removed in a future API version. Use `subscription_details.proration_date` instead.
        """
        subscription_resume_at: NotRequired[Literal["now"]]
        """
        For paused subscriptions, setting `subscription_resume_at` to `now` will preview the invoice that will be generated if the subscription is resumed. This field has been deprecated and will be removed in a future API version. Use `subscription_details.resume_at` instead.
        """
        subscription_start_date: NotRequired[int]
        """
        Date a subscription is intended to start (can be future or past). This field has been deprecated and will be removed in a future API version. Use `subscription_details.start_date` instead.
        """
        subscription_trial_end: NotRequired["Literal['now']|int"]
        """
        If provided, the invoice returned will preview updating or creating a subscription with that trial end. If set, one of `subscription_items` or `subscription` is required. This field has been deprecated and will be removed in a future API version. Use `subscription_details.trial_end` instead.
        """
        subscription_trial_from_plan: NotRequired[bool]
        """
        Indicates if a plan's `trial_period_days` should be applied to the subscription. Setting `subscription_trial_end` per subscription is preferred, and this defaults to `false`. Setting this flag to `true` together with `subscription_trial_end` is not allowed. See [Using trial periods on subscriptions](https://stripe.com/docs/billing/subscriptions/trials) to learn more.
        """

    class UpcomingParamsAutomaticTax(TypedDict):
        enabled: bool
        """
        Whether Stripe automatically computes tax on this invoice. Note that incompatible invoice items (invoice items with manually specified [tax rates](https://stripe.com/docs/api/tax_rates), negative amounts, or `tax_behavior=unspecified`) cannot be added to automatic tax invoices.
        """
        liability: NotRequired[
            "InvoiceService.UpcomingParamsAutomaticTaxLiability"
        ]
        """
        The account that's liable for tax. If set, the business address and tax registrations required to perform the tax calculation are loaded from this account. The tax transaction is returned in the report of the connected account.
        """

    class UpcomingParamsAutomaticTaxLiability(TypedDict):
        account: NotRequired[str]
        """
        The connected account being referenced when `type` is `account`.
        """
        type: Literal["account", "self"]
        """
        Type of the account referenced in the request.
        """

    class UpcomingParamsCustomerDetails(TypedDict):
        address: NotRequired[
            "Literal['']|InvoiceService.UpcomingParamsCustomerDetailsAddress"
        ]
        """
        The customer's address.
        """
        shipping: NotRequired[
            "Literal['']|InvoiceService.UpcomingParamsCustomerDetailsShipping"
        ]
        """
        The customer's shipping information. Appears on invoices emailed to this customer.
        """
        tax: NotRequired["InvoiceService.UpcomingParamsCustomerDetailsTax"]
        """
        Tax details about the customer.
        """
        tax_exempt: NotRequired[
            "Literal['']|Literal['exempt', 'none', 'reverse']"
        ]
        """
        The customer's tax exemption. One of `none`, `exempt`, or `reverse`.
        """
        tax_ids: NotRequired[
            List["InvoiceService.UpcomingParamsCustomerDetailsTaxId"]
        ]
        """
        The customer's tax IDs.
        """

    class UpcomingParamsCustomerDetailsAddress(TypedDict):
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

    class UpcomingParamsCustomerDetailsShipping(TypedDict):
        address: "InvoiceService.UpcomingParamsCustomerDetailsShippingAddress"
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

    class UpcomingParamsCustomerDetailsShippingAddress(TypedDict):
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

    class UpcomingParamsCustomerDetailsTax(TypedDict):
        ip_address: NotRequired["Literal['']|str"]
        """
        A recent IP address of the customer used for tax reporting and tax location inference. Stripe recommends updating the IP address when a new PaymentMethod is attached or the address field on the customer is updated. We recommend against updating this field more frequently since it could result in unexpected tax location/reporting outcomes.
        """

    class UpcomingParamsCustomerDetailsTaxId(TypedDict):
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

    class UpcomingParamsDiscount(TypedDict):
        coupon: NotRequired[str]
        """
        ID of the coupon to create a new discount for.
        """
        discount: NotRequired[str]
        """
        ID of an existing discount on the object (or one of its ancestors) to reuse.
        """
        promotion_code: NotRequired[str]
        """
        ID of the promotion code to create a new discount for.
        """

    class UpcomingParamsInvoiceItem(TypedDict):
        amount: NotRequired[int]
        """
        The integer amount in cents (or local equivalent) of previewed invoice item.
        """
        currency: NotRequired[str]
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies). Only applicable to new invoice items.
        """
        description: NotRequired[str]
        """
        An arbitrary string which you can attach to the invoice item. The description is displayed in the invoice for easy tracking.
        """
        discountable: NotRequired[bool]
        """
        Explicitly controls whether discounts apply to this invoice item. Defaults to true, except for negative invoice items.
        """
        discounts: NotRequired[
            "Literal['']|List[InvoiceService.UpcomingParamsInvoiceItemDiscount]"
        ]
        """
        The coupons to redeem into discounts for the invoice item in the preview.
        """
        invoiceitem: NotRequired[str]
        """
        The ID of the invoice item to update in preview. If not specified, a new invoice item will be added to the preview of the upcoming invoice.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        period: NotRequired["InvoiceService.UpcomingParamsInvoiceItemPeriod"]
        """
        The period associated with this invoice item. When set to different values, the period will be rendered on the invoice. If you have [Stripe Revenue Recognition](https://stripe.com/docs/revenue-recognition) enabled, the period will be used to recognize and defer revenue. See the [Revenue Recognition documentation](https://stripe.com/docs/revenue-recognition/methodology/subscriptions-and-invoicing) for details.
        """
        price: NotRequired[str]
        """
        The ID of the price object. One of `price` or `price_data` is required.
        """
        price_data: NotRequired[
            "InvoiceService.UpcomingParamsInvoiceItemPriceData"
        ]
        """
        Data used to generate a new [Price](https://stripe.com/docs/api/prices) object inline. One of `price` or `price_data` is required.
        """
        quantity: NotRequired[int]
        """
        Non-negative integer. The quantity of units for the invoice item.
        """
        tax_behavior: NotRequired[
            Literal["exclusive", "inclusive", "unspecified"]
        ]
        """
        Only required if a [default tax behavior](https://stripe.com/docs/tax/products-prices-tax-categories-tax-behavior#setting-a-default-tax-behavior-(recommended)) was not provided in the Stripe Tax settings. Specifies whether the price is considered inclusive of taxes or exclusive of taxes. One of `inclusive`, `exclusive`, or `unspecified`. Once specified as either `inclusive` or `exclusive`, it cannot be changed.
        """
        tax_code: NotRequired["Literal['']|str"]
        """
        A [tax code](https://stripe.com/docs/tax/tax-categories) ID.
        """
        tax_rates: NotRequired["Literal['']|List[str]"]
        """
        The tax rates that apply to the item. When set, any `default_tax_rates` do not apply to this item.
        """
        unit_amount: NotRequired[int]
        """
        The integer unit amount in cents (or local equivalent) of the charge to be applied to the upcoming invoice. This unit_amount will be multiplied by the quantity to get the full amount. If you want to apply a credit to the customer's account, pass a negative unit_amount.
        """
        unit_amount_decimal: NotRequired[str]
        """
        Same as `unit_amount`, but accepts a decimal value in cents (or local equivalent) with at most 12 decimal places. Only one of `unit_amount` and `unit_amount_decimal` can be set.
        """

    class UpcomingParamsInvoiceItemDiscount(TypedDict):
        coupon: NotRequired[str]
        """
        ID of the coupon to create a new discount for.
        """
        discount: NotRequired[str]
        """
        ID of an existing discount on the object (or one of its ancestors) to reuse.
        """
        promotion_code: NotRequired[str]
        """
        ID of the promotion code to create a new discount for.
        """

    class UpcomingParamsInvoiceItemPeriod(TypedDict):
        end: int
        """
        The end of the period, which must be greater than or equal to the start. This value is inclusive.
        """
        start: int
        """
        The start of the period. This value is inclusive.
        """

    class UpcomingParamsInvoiceItemPriceData(TypedDict):
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        product: str
        """
        The ID of the product that this price will belong to.
        """
        tax_behavior: NotRequired[
            Literal["exclusive", "inclusive", "unspecified"]
        ]
        """
        Only required if a [default tax behavior](https://stripe.com/docs/tax/products-prices-tax-categories-tax-behavior#setting-a-default-tax-behavior-(recommended)) was not provided in the Stripe Tax settings. Specifies whether the price is considered inclusive of taxes or exclusive of taxes. One of `inclusive`, `exclusive`, or `unspecified`. Once specified as either `inclusive` or `exclusive`, it cannot be changed.
        """
        unit_amount: NotRequired[int]
        """
        A positive integer in cents (or local equivalent) (or 0 for a free price) representing how much to charge.
        """
        unit_amount_decimal: NotRequired[str]
        """
        Same as `unit_amount`, but accepts a decimal value in cents (or local equivalent) with at most 12 decimal places. Only one of `unit_amount` and `unit_amount_decimal` can be set.
        """

    class UpcomingParamsIssuer(TypedDict):
        account: NotRequired[str]
        """
        The connected account being referenced when `type` is `account`.
        """
        type: Literal["account", "self"]
        """
        Type of the account referenced in the request.
        """

    class UpcomingParamsScheduleDetails(TypedDict):
        end_behavior: NotRequired[Literal["cancel", "release"]]
        """
        Behavior of the subscription schedule and underlying subscription when it ends. Possible values are `release` or `cancel` with the default being `release`. `release` will end the subscription schedule and keep the underlying subscription running. `cancel` will end the subscription schedule and cancel the underlying subscription.
        """
        phases: NotRequired[
            List["InvoiceService.UpcomingParamsScheduleDetailsPhase"]
        ]
        """
        List representing phases of the subscription schedule. Each phase can be customized to have different durations, plans, and coupons. If there are multiple phases, the `end_date` of one phase will always equal the `start_date` of the next phase.
        """
        proration_behavior: NotRequired[
            Literal["always_invoice", "create_prorations", "none"]
        ]
        """
        In cases where the `schedule_details` params update the currently active phase, specifies if and how to prorate at the time of the request.
        """

    class UpcomingParamsScheduleDetailsPhase(TypedDict):
        add_invoice_items: NotRequired[
            List[
                "InvoiceService.UpcomingParamsScheduleDetailsPhaseAddInvoiceItem"
            ]
        ]
        """
        A list of prices and quantities that will generate invoice items appended to the next invoice for this phase. You may pass up to 20 items.
        """
        application_fee_percent: NotRequired[float]
        """
        A non-negative decimal between 0 and 100, with at most two decimal places. This represents the percentage of the subscription invoice total that will be transferred to the application owner's Stripe account. The request must be made by a platform account on a connected account in order to set an application fee percentage. For more information, see the application fees [documentation](https://stripe.com/docs/connect/subscriptions#collecting-fees-on-subscriptions).
        """
        automatic_tax: NotRequired[
            "InvoiceService.UpcomingParamsScheduleDetailsPhaseAutomaticTax"
        ]
        """
        Automatic tax settings for this phase.
        """
        billing_cycle_anchor: NotRequired[Literal["automatic", "phase_start"]]
        """
        Can be set to `phase_start` to set the anchor to the start of the phase or `automatic` to automatically change it if needed. Cannot be set to `phase_start` if this phase specifies a trial. For more information, see the billing cycle [documentation](https://stripe.com/docs/billing/subscriptions/billing-cycle).
        """
        billing_thresholds: NotRequired[
            "Literal['']|InvoiceService.UpcomingParamsScheduleDetailsPhaseBillingThresholds"
        ]
        """
        Define thresholds at which an invoice will be sent, and the subscription advanced to a new billing period. Pass an empty string to remove previously-defined thresholds.
        """
        collection_method: NotRequired[
            Literal["charge_automatically", "send_invoice"]
        ]
        """
        Either `charge_automatically`, or `send_invoice`. When charging automatically, Stripe will attempt to pay the underlying subscription at the end of each billing cycle using the default source attached to the customer. When sending an invoice, Stripe will email your customer an invoice with payment instructions and mark the subscription as `active`. Defaults to `charge_automatically` on creation.
        """
        coupon: NotRequired[str]
        """
        The ID of the coupon to apply to this phase of the subscription schedule. This field has been deprecated and will be removed in a future API version. Use `discounts` instead.
        """
        currency: NotRequired[str]
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        default_payment_method: NotRequired[str]
        """
        ID of the default payment method for the subscription schedule. It must belong to the customer associated with the subscription schedule. If not set, invoices will use the default payment method in the customer's invoice settings.
        """
        default_tax_rates: NotRequired["Literal['']|List[str]"]
        """
        A list of [Tax Rate](https://stripe.com/docs/api/tax_rates) ids. These Tax Rates will set the Subscription's [`default_tax_rates`](https://stripe.com/docs/api/subscriptions/create#create_subscription-default_tax_rates), which means they will be the Invoice's [`default_tax_rates`](https://stripe.com/docs/api/invoices/create#create_invoice-default_tax_rates) for any Invoices issued by the Subscription during this Phase.
        """
        description: NotRequired["Literal['']|str"]
        """
        Subscription description, meant to be displayable to the customer. Use this field to optionally store an explanation of the subscription for rendering in Stripe surfaces and certain local payment methods UIs.
        """
        discounts: NotRequired[
            "Literal['']|List[InvoiceService.UpcomingParamsScheduleDetailsPhaseDiscount]"
        ]
        """
        The coupons to redeem into discounts for the schedule phase. If not specified, inherits the discount from the subscription's customer. Pass an empty string to avoid inheriting any discounts.
        """
        end_date: NotRequired["int|Literal['now']"]
        """
        The date at which this phase of the subscription schedule ends. If set, `iterations` must not be set.
        """
        invoice_settings: NotRequired[
            "InvoiceService.UpcomingParamsScheduleDetailsPhaseInvoiceSettings"
        ]
        """
        All invoices will be billed using the specified settings.
        """
        items: List["InvoiceService.UpcomingParamsScheduleDetailsPhaseItem"]
        """
        List of configuration items, each with an attached price, to apply during this phase of the subscription schedule.
        """
        iterations: NotRequired[int]
        """
        Integer representing the multiplier applied to the price interval. For example, `iterations=2` applied to a price with `interval=month` and `interval_count=3` results in a phase of duration `2 * 3 months = 6 months`. If set, `end_date` must not be set.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to a phase. Metadata on a schedule's phase will update the underlying subscription's `metadata` when the phase is entered, adding new keys and replacing existing keys in the subscription's `metadata`. Individual keys in the subscription's `metadata` can be unset by posting an empty value to them in the phase's `metadata`. To unset all keys in the subscription's `metadata`, update the subscription directly or unset every key individually from the phase's `metadata`.
        """
        on_behalf_of: NotRequired[str]
        """
        The account on behalf of which to charge, for each of the associated subscription's invoices.
        """
        proration_behavior: NotRequired[
            Literal["always_invoice", "create_prorations", "none"]
        ]
        """
        Whether the subscription schedule will create [prorations](https://stripe.com/docs/billing/subscriptions/prorations) when transitioning to this phase. The default value is `create_prorations`. This setting controls prorations when a phase is started asynchronously and it is persisted as a field on the phase. It's different from the request-level [proration_behavior](https://stripe.com/docs/api/subscription_schedules/update#update_subscription_schedule-proration_behavior) parameter which controls what happens if the update request affects the billing configuration of the current phase.
        """
        start_date: NotRequired["int|Literal['now']"]
        """
        The date at which this phase of the subscription schedule starts or `now`. Must be set on the first phase.
        """
        transfer_data: NotRequired[
            "InvoiceService.UpcomingParamsScheduleDetailsPhaseTransferData"
        ]
        """
        The data with which to automatically create a Transfer for each of the associated subscription's invoices.
        """
        trial: NotRequired[bool]
        """
        If set to true the entire phase is counted as a trial and the customer will not be charged for any fees.
        """
        trial_end: NotRequired["int|Literal['now']"]
        """
        Sets the phase to trialing from the start date to this date. Must be before the phase end date, can not be combined with `trial`
        """

    class UpcomingParamsScheduleDetailsPhaseAddInvoiceItem(TypedDict):
        discounts: NotRequired[
            List[
                "InvoiceService.UpcomingParamsScheduleDetailsPhaseAddInvoiceItemDiscount"
            ]
        ]
        """
        The coupons to redeem into discounts for the item.
        """
        price: NotRequired[str]
        """
        The ID of the price object. One of `price` or `price_data` is required.
        """
        price_data: NotRequired[
            "InvoiceService.UpcomingParamsScheduleDetailsPhaseAddInvoiceItemPriceData"
        ]
        """
        Data used to generate a new [Price](https://stripe.com/docs/api/prices) object inline. One of `price` or `price_data` is required.
        """
        quantity: NotRequired[int]
        """
        Quantity for this item. Defaults to 1.
        """
        tax_rates: NotRequired["Literal['']|List[str]"]
        """
        The tax rates which apply to the item. When set, the `default_tax_rates` do not apply to this item.
        """

    class UpcomingParamsScheduleDetailsPhaseAddInvoiceItemDiscount(TypedDict):
        coupon: NotRequired[str]
        """
        ID of the coupon to create a new discount for.
        """
        discount: NotRequired[str]
        """
        ID of an existing discount on the object (or one of its ancestors) to reuse.
        """
        promotion_code: NotRequired[str]
        """
        ID of the promotion code to create a new discount for.
        """

    class UpcomingParamsScheduleDetailsPhaseAddInvoiceItemPriceData(TypedDict):
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        product: str
        """
        The ID of the product that this price will belong to.
        """
        tax_behavior: NotRequired[
            Literal["exclusive", "inclusive", "unspecified"]
        ]
        """
        Only required if a [default tax behavior](https://stripe.com/docs/tax/products-prices-tax-categories-tax-behavior#setting-a-default-tax-behavior-(recommended)) was not provided in the Stripe Tax settings. Specifies whether the price is considered inclusive of taxes or exclusive of taxes. One of `inclusive`, `exclusive`, or `unspecified`. Once specified as either `inclusive` or `exclusive`, it cannot be changed.
        """
        unit_amount: NotRequired[int]
        """
        A positive integer in cents (or local equivalent) (or 0 for a free price) representing how much to charge.
        """
        unit_amount_decimal: NotRequired[str]
        """
        Same as `unit_amount`, but accepts a decimal value in cents (or local equivalent) with at most 12 decimal places. Only one of `unit_amount` and `unit_amount_decimal` can be set.
        """

    class UpcomingParamsScheduleDetailsPhaseAutomaticTax(TypedDict):
        enabled: bool
        """
        Enabled automatic tax calculation which will automatically compute tax rates on all invoices generated by the subscription.
        """
        liability: NotRequired[
            "InvoiceService.UpcomingParamsScheduleDetailsPhaseAutomaticTaxLiability"
        ]
        """
        The account that's liable for tax. If set, the business address and tax registrations required to perform the tax calculation are loaded from this account. The tax transaction is returned in the report of the connected account.
        """

    class UpcomingParamsScheduleDetailsPhaseAutomaticTaxLiability(TypedDict):
        account: NotRequired[str]
        """
        The connected account being referenced when `type` is `account`.
        """
        type: Literal["account", "self"]
        """
        Type of the account referenced in the request.
        """

    class UpcomingParamsScheduleDetailsPhaseBillingThresholds(TypedDict):
        amount_gte: NotRequired[int]
        """
        Monetary threshold that triggers the subscription to advance to a new billing period
        """
        reset_billing_cycle_anchor: NotRequired[bool]
        """
        Indicates if the `billing_cycle_anchor` should be reset when a threshold is reached. If true, `billing_cycle_anchor` will be updated to the date/time the threshold was last reached; otherwise, the value will remain unchanged.
        """

    class UpcomingParamsScheduleDetailsPhaseDiscount(TypedDict):
        coupon: NotRequired[str]
        """
        ID of the coupon to create a new discount for.
        """
        discount: NotRequired[str]
        """
        ID of an existing discount on the object (or one of its ancestors) to reuse.
        """
        promotion_code: NotRequired[str]
        """
        ID of the promotion code to create a new discount for.
        """

    class UpcomingParamsScheduleDetailsPhaseInvoiceSettings(TypedDict):
        account_tax_ids: NotRequired["Literal['']|List[str]"]
        """
        The account tax IDs associated with this phase of the subscription schedule. Will be set on invoices generated by this phase of the subscription schedule.
        """
        days_until_due: NotRequired[int]
        """
        Number of days within which a customer must pay invoices generated by this subscription schedule. This value will be `null` for subscription schedules where `billing=charge_automatically`.
        """
        issuer: NotRequired[
            "InvoiceService.UpcomingParamsScheduleDetailsPhaseInvoiceSettingsIssuer"
        ]
        """
        The connected account that issues the invoice. The invoice is presented with the branding and support information of the specified account.
        """

    class UpcomingParamsScheduleDetailsPhaseInvoiceSettingsIssuer(TypedDict):
        account: NotRequired[str]
        """
        The connected account being referenced when `type` is `account`.
        """
        type: Literal["account", "self"]
        """
        Type of the account referenced in the request.
        """

    class UpcomingParamsScheduleDetailsPhaseItem(TypedDict):
        billing_thresholds: NotRequired[
            "Literal['']|InvoiceService.UpcomingParamsScheduleDetailsPhaseItemBillingThresholds"
        ]
        """
        Define thresholds at which an invoice will be sent, and the subscription advanced to a new billing period. When updating, pass an empty string to remove previously-defined thresholds.
        """
        discounts: NotRequired[
            "Literal['']|List[InvoiceService.UpcomingParamsScheduleDetailsPhaseItemDiscount]"
        ]
        """
        The coupons to redeem into discounts for the subscription item.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to a configuration item. Metadata on a configuration item will update the underlying subscription item's `metadata` when the phase is entered, adding new keys and replacing existing keys. Individual keys in the subscription item's `metadata` can be unset by posting an empty value to them in the configuration item's `metadata`. To unset all keys in the subscription item's `metadata`, update the subscription item directly or unset every key individually from the configuration item's `metadata`.
        """
        plan: NotRequired[str]
        """
        The plan ID to subscribe to. You may specify the same ID in `plan` and `price`.
        """
        price: NotRequired[str]
        """
        The ID of the price object.
        """
        price_data: NotRequired[
            "InvoiceService.UpcomingParamsScheduleDetailsPhaseItemPriceData"
        ]
        """
        Data used to generate a new [Price](https://stripe.com/docs/api/prices) object inline.
        """
        quantity: NotRequired[int]
        """
        Quantity for the given price. Can be set only if the price's `usage_type` is `licensed` and not `metered`.
        """
        tax_rates: NotRequired["Literal['']|List[str]"]
        """
        A list of [Tax Rate](https://stripe.com/docs/api/tax_rates) ids. These Tax Rates will override the [`default_tax_rates`](https://stripe.com/docs/api/subscriptions/create#create_subscription-default_tax_rates) on the Subscription. When updating, pass an empty string to remove previously-defined tax rates.
        """

    class UpcomingParamsScheduleDetailsPhaseItemBillingThresholds(TypedDict):
        usage_gte: int
        """
        Number of units that meets the billing threshold to advance the subscription to a new billing period (e.g., it takes 10 $5 units to meet a $50 [monetary threshold](https://stripe.com/docs/api/subscriptions/update#update_subscription-billing_thresholds-amount_gte))
        """

    class UpcomingParamsScheduleDetailsPhaseItemDiscount(TypedDict):
        coupon: NotRequired[str]
        """
        ID of the coupon to create a new discount for.
        """
        discount: NotRequired[str]
        """
        ID of an existing discount on the object (or one of its ancestors) to reuse.
        """
        promotion_code: NotRequired[str]
        """
        ID of the promotion code to create a new discount for.
        """

    class UpcomingParamsScheduleDetailsPhaseItemPriceData(TypedDict):
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        product: str
        """
        The ID of the product that this price will belong to.
        """
        recurring: "InvoiceService.UpcomingParamsScheduleDetailsPhaseItemPriceDataRecurring"
        """
        The recurring components of a price such as `interval` and `interval_count`.
        """
        tax_behavior: NotRequired[
            Literal["exclusive", "inclusive", "unspecified"]
        ]
        """
        Only required if a [default tax behavior](https://stripe.com/docs/tax/products-prices-tax-categories-tax-behavior#setting-a-default-tax-behavior-(recommended)) was not provided in the Stripe Tax settings. Specifies whether the price is considered inclusive of taxes or exclusive of taxes. One of `inclusive`, `exclusive`, or `unspecified`. Once specified as either `inclusive` or `exclusive`, it cannot be changed.
        """
        unit_amount: NotRequired[int]
        """
        A positive integer in cents (or local equivalent) (or 0 for a free price) representing how much to charge.
        """
        unit_amount_decimal: NotRequired[str]
        """
        Same as `unit_amount`, but accepts a decimal value in cents (or local equivalent) with at most 12 decimal places. Only one of `unit_amount` and `unit_amount_decimal` can be set.
        """

    class UpcomingParamsScheduleDetailsPhaseItemPriceDataRecurring(TypedDict):
        interval: Literal["day", "month", "week", "year"]
        """
        Specifies billing frequency. Either `day`, `week`, `month` or `year`.
        """
        interval_count: NotRequired[int]
        """
        The number of intervals between subscription billings. For example, `interval=month` and `interval_count=3` bills every 3 months. Maximum of three years interval allowed (3 years, 36 months, or 156 weeks).
        """

    class UpcomingParamsScheduleDetailsPhaseTransferData(TypedDict):
        amount_percent: NotRequired[float]
        """
        A non-negative decimal between 0 and 100, with at most two decimal places. This represents the percentage of the subscription invoice total that will be transferred to the destination account. By default, the entire amount is transferred to the destination.
        """
        destination: str
        """
        ID of an existing, connected Stripe account.
        """

    class UpcomingParamsSubscriptionDetails(TypedDict):
        billing_cycle_anchor: NotRequired["Literal['now', 'unchanged']|int"]
        """
        For new subscriptions, a future timestamp to anchor the subscription's [billing cycle](https://stripe.com/docs/subscriptions/billing-cycle). This is used to determine the date of the first full invoice, and, for plans with `month` or `year` intervals, the day of the month for subsequent invoices. For existing subscriptions, the value can only be set to `now` or `unchanged`.
        """
        cancel_at: NotRequired["Literal['']|int"]
        """
        A timestamp at which the subscription should cancel. If set to a date before the current period ends, this will cause a proration if prorations have been enabled using `proration_behavior`. If set during a future period, this will always cause a proration for that period.
        """
        cancel_at_period_end: NotRequired[bool]
        """
        Indicate whether this subscription should cancel at the end of the current period (`current_period_end`). Defaults to `false`.
        """
        cancel_now: NotRequired[bool]
        """
        This simulates the subscription being canceled or expired immediately.
        """
        default_tax_rates: NotRequired["Literal['']|List[str]"]
        """
        If provided, the invoice returned will preview updating or creating a subscription with these default tax rates. The default tax rates will apply to any line item that does not have `tax_rates` set.
        """
        items: NotRequired[
            List["InvoiceService.UpcomingParamsSubscriptionDetailsItem"]
        ]
        """
        A list of up to 20 subscription items, each with an attached price.
        """
        proration_behavior: NotRequired[
            Literal["always_invoice", "create_prorations", "none"]
        ]
        """
        Determines how to handle [prorations](https://stripe.com/docs/billing/subscriptions/prorations) when the billing cycle changes (e.g., when switching plans, resetting `billing_cycle_anchor=now`, or starting a trial), or if an item's `quantity` changes. The default value is `create_prorations`.
        """
        proration_date: NotRequired[int]
        """
        If previewing an update to a subscription, and doing proration, `subscription_details.proration_date` forces the proration to be calculated as though the update was done at the specified time. The time given must be within the current subscription period and within the current phase of the schedule backing this subscription, if the schedule exists. If set, `subscription`, and one of `subscription_details.items`, or `subscription_details.trial_end` are required. Also, `subscription_details.proration_behavior` cannot be set to 'none'.
        """
        resume_at: NotRequired[Literal["now"]]
        """
        For paused subscriptions, setting `subscription_details.resume_at` to `now` will preview the invoice that will be generated if the subscription is resumed.
        """
        start_date: NotRequired[int]
        """
        Date a subscription is intended to start (can be future or past).
        """
        trial_end: NotRequired["Literal['now']|int"]
        """
        If provided, the invoice returned will preview updating or creating a subscription with that trial end. If set, one of `subscription_details.items` or `subscription` is required.
        """

    class UpcomingParamsSubscriptionDetailsItem(TypedDict):
        billing_thresholds: NotRequired[
            "Literal['']|InvoiceService.UpcomingParamsSubscriptionDetailsItemBillingThresholds"
        ]
        """
        Define thresholds at which an invoice will be sent, and the subscription advanced to a new billing period. When updating, pass an empty string to remove previously-defined thresholds.
        """
        clear_usage: NotRequired[bool]
        """
        Delete all usage for a given subscription item. Allowed only when `deleted` is set to `true` and the current plan's `usage_type` is `metered`.
        """
        deleted: NotRequired[bool]
        """
        A flag that, if set to `true`, will delete the specified item.
        """
        discounts: NotRequired[
            "Literal['']|List[InvoiceService.UpcomingParamsSubscriptionDetailsItemDiscount]"
        ]
        """
        The coupons to redeem into discounts for the subscription item.
        """
        id: NotRequired[str]
        """
        Subscription item to update.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        plan: NotRequired[str]
        """
        Plan ID for this item, as a string.
        """
        price: NotRequired[str]
        """
        The ID of the price object. One of `price` or `price_data` is required. When changing a subscription item's price, `quantity` is set to 1 unless a `quantity` parameter is provided.
        """
        price_data: NotRequired[
            "InvoiceService.UpcomingParamsSubscriptionDetailsItemPriceData"
        ]
        """
        Data used to generate a new [Price](https://stripe.com/docs/api/prices) object inline. One of `price` or `price_data` is required.
        """
        quantity: NotRequired[int]
        """
        Quantity for this item.
        """
        tax_rates: NotRequired["Literal['']|List[str]"]
        """
        A list of [Tax Rate](https://stripe.com/docs/api/tax_rates) ids. These Tax Rates will override the [`default_tax_rates`](https://stripe.com/docs/api/subscriptions/create#create_subscription-default_tax_rates) on the Subscription. When updating, pass an empty string to remove previously-defined tax rates.
        """

    class UpcomingParamsSubscriptionDetailsItemBillingThresholds(TypedDict):
        usage_gte: int
        """
        Number of units that meets the billing threshold to advance the subscription to a new billing period (e.g., it takes 10 $5 units to meet a $50 [monetary threshold](https://stripe.com/docs/api/subscriptions/update#update_subscription-billing_thresholds-amount_gte))
        """

    class UpcomingParamsSubscriptionDetailsItemDiscount(TypedDict):
        coupon: NotRequired[str]
        """
        ID of the coupon to create a new discount for.
        """
        discount: NotRequired[str]
        """
        ID of an existing discount on the object (or one of its ancestors) to reuse.
        """
        promotion_code: NotRequired[str]
        """
        ID of the promotion code to create a new discount for.
        """

    class UpcomingParamsSubscriptionDetailsItemPriceData(TypedDict):
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        product: str
        """
        The ID of the product that this price will belong to.
        """
        recurring: "InvoiceService.UpcomingParamsSubscriptionDetailsItemPriceDataRecurring"
        """
        The recurring components of a price such as `interval` and `interval_count`.
        """
        tax_behavior: NotRequired[
            Literal["exclusive", "inclusive", "unspecified"]
        ]
        """
        Only required if a [default tax behavior](https://stripe.com/docs/tax/products-prices-tax-categories-tax-behavior#setting-a-default-tax-behavior-(recommended)) was not provided in the Stripe Tax settings. Specifies whether the price is considered inclusive of taxes or exclusive of taxes. One of `inclusive`, `exclusive`, or `unspecified`. Once specified as either `inclusive` or `exclusive`, it cannot be changed.
        """
        unit_amount: NotRequired[int]
        """
        A positive integer in cents (or local equivalent) (or 0 for a free price) representing how much to charge.
        """
        unit_amount_decimal: NotRequired[str]
        """
        Same as `unit_amount`, but accepts a decimal value in cents (or local equivalent) with at most 12 decimal places. Only one of `unit_amount` and `unit_amount_decimal` can be set.
        """

    class UpcomingParamsSubscriptionDetailsItemPriceDataRecurring(TypedDict):
        interval: Literal["day", "month", "week", "year"]
        """
        Specifies billing frequency. Either `day`, `week`, `month` or `year`.
        """
        interval_count: NotRequired[int]
        """
        The number of intervals between subscription billings. For example, `interval=month` and `interval_count=3` bills every 3 months. Maximum of three years interval allowed (3 years, 36 months, or 156 weeks).
        """

    class UpcomingParamsSubscriptionItem(TypedDict):
        billing_thresholds: NotRequired[
            "Literal['']|InvoiceService.UpcomingParamsSubscriptionItemBillingThresholds"
        ]
        """
        Define thresholds at which an invoice will be sent, and the subscription advanced to a new billing period. When updating, pass an empty string to remove previously-defined thresholds.
        """
        clear_usage: NotRequired[bool]
        """
        Delete all usage for a given subscription item. Allowed only when `deleted` is set to `true` and the current plan's `usage_type` is `metered`.
        """
        deleted: NotRequired[bool]
        """
        A flag that, if set to `true`, will delete the specified item.
        """
        discounts: NotRequired[
            "Literal['']|List[InvoiceService.UpcomingParamsSubscriptionItemDiscount]"
        ]
        """
        The coupons to redeem into discounts for the subscription item.
        """
        id: NotRequired[str]
        """
        Subscription item to update.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        plan: NotRequired[str]
        """
        Plan ID for this item, as a string.
        """
        price: NotRequired[str]
        """
        The ID of the price object. One of `price` or `price_data` is required. When changing a subscription item's price, `quantity` is set to 1 unless a `quantity` parameter is provided.
        """
        price_data: NotRequired[
            "InvoiceService.UpcomingParamsSubscriptionItemPriceData"
        ]
        """
        Data used to generate a new [Price](https://stripe.com/docs/api/prices) object inline. One of `price` or `price_data` is required.
        """
        quantity: NotRequired[int]
        """
        Quantity for this item.
        """
        tax_rates: NotRequired["Literal['']|List[str]"]
        """
        A list of [Tax Rate](https://stripe.com/docs/api/tax_rates) ids. These Tax Rates will override the [`default_tax_rates`](https://stripe.com/docs/api/subscriptions/create#create_subscription-default_tax_rates) on the Subscription. When updating, pass an empty string to remove previously-defined tax rates.
        """

    class UpcomingParamsSubscriptionItemBillingThresholds(TypedDict):
        usage_gte: int
        """
        Number of units that meets the billing threshold to advance the subscription to a new billing period (e.g., it takes 10 $5 units to meet a $50 [monetary threshold](https://stripe.com/docs/api/subscriptions/update#update_subscription-billing_thresholds-amount_gte))
        """

    class UpcomingParamsSubscriptionItemDiscount(TypedDict):
        coupon: NotRequired[str]
        """
        ID of the coupon to create a new discount for.
        """
        discount: NotRequired[str]
        """
        ID of an existing discount on the object (or one of its ancestors) to reuse.
        """
        promotion_code: NotRequired[str]
        """
        ID of the promotion code to create a new discount for.
        """

    class UpcomingParamsSubscriptionItemPriceData(TypedDict):
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        product: str
        """
        The ID of the product that this price will belong to.
        """
        recurring: (
            "InvoiceService.UpcomingParamsSubscriptionItemPriceDataRecurring"
        )
        """
        The recurring components of a price such as `interval` and `interval_count`.
        """
        tax_behavior: NotRequired[
            Literal["exclusive", "inclusive", "unspecified"]
        ]
        """
        Only required if a [default tax behavior](https://stripe.com/docs/tax/products-prices-tax-categories-tax-behavior#setting-a-default-tax-behavior-(recommended)) was not provided in the Stripe Tax settings. Specifies whether the price is considered inclusive of taxes or exclusive of taxes. One of `inclusive`, `exclusive`, or `unspecified`. Once specified as either `inclusive` or `exclusive`, it cannot be changed.
        """
        unit_amount: NotRequired[int]
        """
        A positive integer in cents (or local equivalent) (or 0 for a free price) representing how much to charge.
        """
        unit_amount_decimal: NotRequired[str]
        """
        Same as `unit_amount`, but accepts a decimal value in cents (or local equivalent) with at most 12 decimal places. Only one of `unit_amount` and `unit_amount_decimal` can be set.
        """

    class UpcomingParamsSubscriptionItemPriceDataRecurring(TypedDict):
        interval: Literal["day", "month", "week", "year"]
        """
        Specifies billing frequency. Either `day`, `week`, `month` or `year`.
        """
        interval_count: NotRequired[int]
        """
        The number of intervals between subscription billings. For example, `interval=month` and `interval_count=3` bills every 3 months. Maximum of three years interval allowed (3 years, 36 months, or 156 weeks).
        """

    class UpdateLinesParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        invoice_metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`. For [type=subscription](https://stripe.com/docs/api/invoices/line_item#invoice_line_item_object-type) line items, the incoming metadata specified on the request is directly used to set this value, in contrast to [type=invoiceitem](api/invoices/line_item#invoice_line_item_object-type) line items, where any existing metadata on the invoice line is merged with the incoming data.
        """
        lines: List["InvoiceService.UpdateLinesParamsLine"]
        """
        The line items to update.
        """

    class UpdateLinesParamsLine(TypedDict):
        amount: NotRequired[int]
        """
        The integer amount in cents (or local equivalent) of the charge to be applied to the upcoming invoice. If you want to apply a credit to the customer's account, pass a negative amount.
        """
        description: NotRequired[str]
        """
        An arbitrary string which you can attach to the invoice item. The description is displayed in the invoice for easy tracking.
        """
        discountable: NotRequired[bool]
        """
        Controls whether discounts apply to this line item. Defaults to false for prorations or negative line items, and true for all other line items. Cannot be set to true for prorations.
        """
        discounts: NotRequired[
            "Literal['']|List[InvoiceService.UpdateLinesParamsLineDiscount]"
        ]
        """
        The coupons, promotion codes & existing discounts which apply to the line item. Item discounts are applied before invoice discounts. Pass an empty string to remove previously-defined discounts.
        """
        id: str
        """
        ID of an existing line item on the invoice.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`. For [type=subscription](https://stripe.com/docs/api/invoices/line_item#invoice_line_item_object-type) line items, the incoming metadata specified on the request is directly used to set this value, in contrast to [type=invoiceitem](api/invoices/line_item#invoice_line_item_object-type) line items, where any existing metadata on the invoice line is merged with the incoming data.
        """
        period: NotRequired["InvoiceService.UpdateLinesParamsLinePeriod"]
        """
        The period associated with this invoice item. When set to different values, the period will be rendered on the invoice. If you have [Stripe Revenue Recognition](https://stripe.com/docs/revenue-recognition) enabled, the period will be used to recognize and defer revenue. See the [Revenue Recognition documentation](https://stripe.com/docs/revenue-recognition/methodology/subscriptions-and-invoicing) for details.
        """
        price: NotRequired[str]
        """
        The ID of the price object. One of `price` or `price_data` is required.
        """
        price_data: NotRequired[
            "InvoiceService.UpdateLinesParamsLinePriceData"
        ]
        """
        Data used to generate a new [Price](https://stripe.com/docs/api/prices) object inline. One of `price` or `price_data` is required.
        """
        quantity: NotRequired[int]
        """
        Non-negative integer. The quantity of units for the line item.
        """
        tax_amounts: NotRequired[
            "Literal['']|List[InvoiceService.UpdateLinesParamsLineTaxAmount]"
        ]
        """
        A list of up to 10 tax amounts for this line item. This can be useful if you calculate taxes on your own or use a third-party to calculate them. You cannot set tax amounts if any line item has [tax_rates](https://stripe.com/docs/api/invoices/line_item#invoice_line_item_object-tax_rates) or if the invoice has [default_tax_rates](https://stripe.com/docs/api/invoices/object#invoice_object-default_tax_rates) or uses [automatic tax](https://stripe.com/docs/tax/invoicing). Pass an empty string to remove previously defined tax amounts.
        """
        tax_rates: NotRequired["Literal['']|List[str]"]
        """
        The tax rates which apply to the line item. When set, the `default_tax_rates` on the invoice do not apply to this line item. Pass an empty string to remove previously-defined tax rates.
        """

    class UpdateLinesParamsLineDiscount(TypedDict):
        coupon: NotRequired[str]
        """
        ID of the coupon to create a new discount for.
        """
        discount: NotRequired[str]
        """
        ID of an existing discount on the object (or one of its ancestors) to reuse.
        """
        promotion_code: NotRequired[str]
        """
        ID of the promotion code to create a new discount for.
        """

    class UpdateLinesParamsLinePeriod(TypedDict):
        end: int
        """
        The end of the period, which must be greater than or equal to the start. This value is inclusive.
        """
        start: int
        """
        The start of the period. This value is inclusive.
        """

    class UpdateLinesParamsLinePriceData(TypedDict):
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        product: NotRequired[str]
        """
        The ID of the product that this price will belong to. One of `product` or `product_data` is required.
        """
        product_data: NotRequired[
            "InvoiceService.UpdateLinesParamsLinePriceDataProductData"
        ]
        """
        Data used to generate a new product object inline. One of `product` or `product_data` is required.
        """
        tax_behavior: NotRequired[
            Literal["exclusive", "inclusive", "unspecified"]
        ]
        """
        Only required if a [default tax behavior](https://stripe.com/docs/tax/products-prices-tax-categories-tax-behavior#setting-a-default-tax-behavior-(recommended)) was not provided in the Stripe Tax settings. Specifies whether the price is considered inclusive of taxes or exclusive of taxes. One of `inclusive`, `exclusive`, or `unspecified`. Once specified as either `inclusive` or `exclusive`, it cannot be changed.
        """
        unit_amount: NotRequired[int]
        """
        A non-negative integer in cents (or local equivalent) representing how much to charge. One of `unit_amount` or `unit_amount_decimal` is required.
        """
        unit_amount_decimal: NotRequired[str]
        """
        Same as `unit_amount`, but accepts a decimal value in cents (or local equivalent) with at most 12 decimal places. Only one of `unit_amount` and `unit_amount_decimal` can be set.
        """

    class UpdateLinesParamsLinePriceDataProductData(TypedDict):
        description: NotRequired[str]
        """
        The product's description, meant to be displayable to the customer. Use this field to optionally store a long form explanation of the product being sold for your own rendering purposes.
        """
        images: NotRequired[List[str]]
        """
        A list of up to 8 URLs of images for this product, meant to be displayable to the customer.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        name: str
        """
        The product's name, meant to be displayable to the customer.
        """
        tax_code: NotRequired[str]
        """
        A [tax code](https://stripe.com/docs/tax/tax-categories) ID.
        """

    class UpdateLinesParamsLineTaxAmount(TypedDict):
        amount: int
        """
        The amount, in cents (or local equivalent), of the tax.
        """
        tax_rate_data: (
            "InvoiceService.UpdateLinesParamsLineTaxAmountTaxRateData"
        )
        """
        Data to find or create a TaxRate object.

        Stripe automatically creates or reuses a TaxRate object for each tax amount. If the `tax_rate_data` exactly matches a previous value, Stripe will reuse the TaxRate object. TaxRate objects created automatically by Stripe are immediately archived, do not appear in the line item's `tax_rates`, and cannot be directly added to invoices, payments, or line items.
        """
        taxable_amount: int
        """
        The amount on which tax is calculated, in cents (or local equivalent).
        """

    class UpdateLinesParamsLineTaxAmountTaxRateData(TypedDict):
        country: NotRequired[str]
        """
        Two-letter country code ([ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)).
        """
        description: NotRequired[str]
        """
        An arbitrary string attached to the tax rate for your internal use only. It will not be visible to your customers.
        """
        display_name: str
        """
        The display name of the tax rate, which will be shown to users.
        """
        inclusive: bool
        """
        This specifies if the tax rate is inclusive or exclusive.
        """
        jurisdiction: NotRequired[str]
        """
        The jurisdiction for the tax rate. You can use this label field for tax reporting purposes. It also appears on your customer's invoice.
        """
        percentage: float
        """
        The statutory tax rate percent. This field accepts decimal values between 0 and 100 inclusive with at most 4 decimal places. To accommodate fixed-amount taxes, set the percentage to zero. Stripe will not display zero percentages on the invoice unless the `amount` of the tax is also zero.
        """
        state: NotRequired[str]
        """
        [ISO 3166-2 subdivision code](https://en.wikipedia.org/wiki/ISO_3166-2:US), without country prefix. For example, "NY" for New York, United States.
        """
        tax_type: NotRequired[
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
        The high-level tax type, such as `vat` or `sales_tax`.
        """

    class UpdateParams(TypedDict):
        account_tax_ids: NotRequired["Literal['']|List[str]"]
        """
        The account tax IDs associated with the invoice. Only editable when the invoice is a draft.
        """
        application_fee_amount: NotRequired[int]
        """
        A fee in cents (or local equivalent) that will be applied to the invoice and transferred to the application owner's Stripe account. The request must be made with an OAuth key or the Stripe-Account header in order to take an application fee. For more information, see the application fees [documentation](https://stripe.com/docs/billing/invoices/connect#collecting-fees).
        """
        auto_advance: NotRequired[bool]
        """
        Controls whether Stripe performs [automatic collection](https://stripe.com/docs/invoicing/integration/automatic-advancement-collection) of the invoice.
        """
        automatic_tax: NotRequired["InvoiceService.UpdateParamsAutomaticTax"]
        """
        Settings for automatic tax lookup for this invoice.
        """
        collection_method: NotRequired[
            Literal["charge_automatically", "send_invoice"]
        ]
        """
        Either `charge_automatically` or `send_invoice`. This field can be updated only on `draft` invoices.
        """
        custom_fields: NotRequired[
            "Literal['']|List[InvoiceService.UpdateParamsCustomField]"
        ]
        """
        A list of up to 4 custom fields to be displayed on the invoice. If a value for `custom_fields` is specified, the list specified will replace the existing custom field list on this invoice. Pass an empty string to remove previously-defined fields.
        """
        days_until_due: NotRequired[int]
        """
        The number of days from which the invoice is created until it is due. Only valid for invoices where `collection_method=send_invoice`. This field can only be updated on `draft` invoices.
        """
        default_payment_method: NotRequired[str]
        """
        ID of the default payment method for the invoice. It must belong to the customer associated with the invoice. If not set, defaults to the subscription's default payment method, if any, or to the default payment method in the customer's invoice settings.
        """
        default_source: NotRequired["Literal['']|str"]
        """
        ID of the default payment source for the invoice. It must belong to the customer associated with the invoice and be in a chargeable state. If not set, defaults to the subscription's default source, if any, or to the customer's default source.
        """
        default_tax_rates: NotRequired["Literal['']|List[str]"]
        """
        The tax rates that will apply to any line item that does not have `tax_rates` set. Pass an empty string to remove previously-defined tax rates.
        """
        description: NotRequired[str]
        """
        An arbitrary string attached to the object. Often useful for displaying to users. Referenced as 'memo' in the Dashboard.
        """
        discounts: NotRequired[
            "Literal['']|List[InvoiceService.UpdateParamsDiscount]"
        ]
        """
        The discounts that will apply to the invoice. Pass an empty string to remove previously-defined discounts.
        """
        due_date: NotRequired[int]
        """
        The date on which payment for this invoice is due. Only valid for invoices where `collection_method=send_invoice`. This field can only be updated on `draft` invoices.
        """
        effective_at: NotRequired["Literal['']|int"]
        """
        The date when this invoice is in effect. Same as `finalized_at` unless overwritten. When defined, this value replaces the system-generated 'Date of issue' printed on the invoice PDF and receipt.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        footer: NotRequired[str]
        """
        Footer to be displayed on the invoice.
        """
        issuer: NotRequired["InvoiceService.UpdateParamsIssuer"]
        """
        The connected account that issues the invoice. The invoice is presented with the branding and support information of the specified account.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        number: NotRequired["Literal['']|str"]
        """
        Set the number for this invoice. If no number is present then a number will be assigned automatically when the invoice is finalized. In many markets, regulations require invoices to be unique, sequential and / or gapless. You are responsible for ensuring this is true across all your different invoicing systems in the event that you edit the invoice number using our API. If you use only Stripe for your invoices and do not change invoice numbers, Stripe handles this aspect of compliance for you automatically.
        """
        on_behalf_of: NotRequired["Literal['']|str"]
        """
        The account (if any) for which the funds of the invoice payment are intended. If set, the invoice will be presented with the branding and support information of the specified account. See the [Invoices with Connect](https://stripe.com/docs/billing/invoices/connect) documentation for details.
        """
        payment_settings: NotRequired[
            "InvoiceService.UpdateParamsPaymentSettings"
        ]
        """
        Configuration settings for the PaymentIntent that is generated when the invoice is finalized.
        """
        rendering: NotRequired["InvoiceService.UpdateParamsRendering"]
        """
        The rendering-related settings that control how the invoice is displayed on customer-facing surfaces such as PDF and Hosted Invoice Page.
        """
        shipping_cost: NotRequired[
            "Literal['']|InvoiceService.UpdateParamsShippingCost"
        ]
        """
        Settings for the cost of shipping for this invoice.
        """
        shipping_details: NotRequired[
            "Literal['']|InvoiceService.UpdateParamsShippingDetails"
        ]
        """
        Shipping details for the invoice. The Invoice PDF will use the `shipping_details` value if it is set, otherwise the PDF will render the shipping address from the customer.
        """
        statement_descriptor: NotRequired[str]
        """
        Extra information about a charge for the customer's credit card statement. It must contain at least one letter. If not specified and this invoice is part of a subscription, the default `statement_descriptor` will be set to the first subscription item's product's `statement_descriptor`.
        """
        transfer_data: NotRequired[
            "Literal['']|InvoiceService.UpdateParamsTransferData"
        ]
        """
        If specified, the funds from the invoice will be transferred to the destination and the ID of the resulting transfer will be found on the invoice's charge. This will be unset if you POST an empty value.
        """

    class UpdateParamsAutomaticTax(TypedDict):
        enabled: bool
        """
        Whether Stripe automatically computes tax on this invoice. Note that incompatible invoice items (invoice items with manually specified [tax rates](https://stripe.com/docs/api/tax_rates), negative amounts, or `tax_behavior=unspecified`) cannot be added to automatic tax invoices.
        """
        liability: NotRequired[
            "InvoiceService.UpdateParamsAutomaticTaxLiability"
        ]
        """
        The account that's liable for tax. If set, the business address and tax registrations required to perform the tax calculation are loaded from this account. The tax transaction is returned in the report of the connected account.
        """

    class UpdateParamsAutomaticTaxLiability(TypedDict):
        account: NotRequired[str]
        """
        The connected account being referenced when `type` is `account`.
        """
        type: Literal["account", "self"]
        """
        Type of the account referenced in the request.
        """

    class UpdateParamsCustomField(TypedDict):
        name: str
        """
        The name of the custom field. This may be up to 40 characters.
        """
        value: str
        """
        The value of the custom field. This may be up to 140 characters.
        """

    class UpdateParamsDiscount(TypedDict):
        coupon: NotRequired[str]
        """
        ID of the coupon to create a new discount for.
        """
        discount: NotRequired[str]
        """
        ID of an existing discount on the object (or one of its ancestors) to reuse.
        """
        promotion_code: NotRequired[str]
        """
        ID of the promotion code to create a new discount for.
        """

    class UpdateParamsIssuer(TypedDict):
        account: NotRequired[str]
        """
        The connected account being referenced when `type` is `account`.
        """
        type: Literal["account", "self"]
        """
        Type of the account referenced in the request.
        """

    class UpdateParamsPaymentSettings(TypedDict):
        default_mandate: NotRequired["Literal['']|str"]
        """
        ID of the mandate to be used for this invoice. It must correspond to the payment method used to pay the invoice, including the invoice's default_payment_method or default_source, if set.
        """
        payment_method_options: NotRequired[
            "InvoiceService.UpdateParamsPaymentSettingsPaymentMethodOptions"
        ]
        """
        Payment-method-specific configuration to provide to the invoice's PaymentIntent.
        """
        payment_method_types: NotRequired[
            "Literal['']|List[Literal['ach_credit_transfer', 'ach_debit', 'acss_debit', 'amazon_pay', 'au_becs_debit', 'bacs_debit', 'bancontact', 'boleto', 'card', 'cashapp', 'customer_balance', 'eps', 'fpx', 'giropay', 'grabpay', 'ideal', 'konbini', 'link', 'multibanco', 'p24', 'paynow', 'paypal', 'promptpay', 'revolut_pay', 'sepa_credit_transfer', 'sepa_debit', 'sofort', 'swish', 'us_bank_account', 'wechat_pay']]"
        ]
        """
        The list of payment method types (e.g. card) to provide to the invoice's PaymentIntent. If not set, Stripe attempts to automatically determine the types to use by looking at the invoice's default payment method, the subscription's default payment method, the customer's default payment method, and your [invoice template settings](https://dashboard.stripe.com/settings/billing/invoice).
        """

    class UpdateParamsPaymentSettingsPaymentMethodOptions(TypedDict):
        acss_debit: NotRequired[
            "Literal['']|InvoiceService.UpdateParamsPaymentSettingsPaymentMethodOptionsAcssDebit"
        ]
        """
        If paying by `acss_debit`, this sub-hash contains details about the Canadian pre-authorized debit payment method options to pass to the invoice's PaymentIntent.
        """
        bancontact: NotRequired[
            "Literal['']|InvoiceService.UpdateParamsPaymentSettingsPaymentMethodOptionsBancontact"
        ]
        """
        If paying by `bancontact`, this sub-hash contains details about the Bancontact payment method options to pass to the invoice's PaymentIntent.
        """
        card: NotRequired[
            "Literal['']|InvoiceService.UpdateParamsPaymentSettingsPaymentMethodOptionsCard"
        ]
        """
        If paying by `card`, this sub-hash contains details about the Card payment method options to pass to the invoice's PaymentIntent.
        """
        customer_balance: NotRequired[
            "Literal['']|InvoiceService.UpdateParamsPaymentSettingsPaymentMethodOptionsCustomerBalance"
        ]
        """
        If paying by `customer_balance`, this sub-hash contains details about the Bank transfer payment method options to pass to the invoice's PaymentIntent.
        """
        konbini: NotRequired[
            "Literal['']|InvoiceService.UpdateParamsPaymentSettingsPaymentMethodOptionsKonbini"
        ]
        """
        If paying by `konbini`, this sub-hash contains details about the Konbini payment method options to pass to the invoice's PaymentIntent.
        """
        sepa_debit: NotRequired[
            "Literal['']|InvoiceService.UpdateParamsPaymentSettingsPaymentMethodOptionsSepaDebit"
        ]
        """
        If paying by `sepa_debit`, this sub-hash contains details about the SEPA Direct Debit payment method options to pass to the invoice's PaymentIntent.
        """
        us_bank_account: NotRequired[
            "Literal['']|InvoiceService.UpdateParamsPaymentSettingsPaymentMethodOptionsUsBankAccount"
        ]
        """
        If paying by `us_bank_account`, this sub-hash contains details about the ACH direct debit payment method options to pass to the invoice's PaymentIntent.
        """

    class UpdateParamsPaymentSettingsPaymentMethodOptionsAcssDebit(TypedDict):
        mandate_options: NotRequired[
            "InvoiceService.UpdateParamsPaymentSettingsPaymentMethodOptionsAcssDebitMandateOptions"
        ]
        """
        Additional fields for Mandate creation
        """
        verification_method: NotRequired[
            Literal["automatic", "instant", "microdeposits"]
        ]
        """
        Verification method for the intent
        """

    class UpdateParamsPaymentSettingsPaymentMethodOptionsAcssDebitMandateOptions(
        TypedDict,
    ):
        transaction_type: NotRequired[Literal["business", "personal"]]
        """
        Transaction type of the mandate.
        """

    class UpdateParamsPaymentSettingsPaymentMethodOptionsBancontact(TypedDict):
        preferred_language: NotRequired[Literal["de", "en", "fr", "nl"]]
        """
        Preferred language of the Bancontact authorization page that the customer is redirected to.
        """

    class UpdateParamsPaymentSettingsPaymentMethodOptionsCard(TypedDict):
        installments: NotRequired[
            "InvoiceService.UpdateParamsPaymentSettingsPaymentMethodOptionsCardInstallments"
        ]
        """
        Installment configuration for payments attempted on this invoice (Mexico Only).

        For more information, see the [installments integration guide](https://stripe.com/docs/payments/installments).
        """
        request_three_d_secure: NotRequired[
            Literal["any", "automatic", "challenge"]
        ]
        """
        We strongly recommend that you rely on our SCA Engine to automatically prompt your customers for authentication based on risk level and [other requirements](https://stripe.com/docs/strong-customer-authentication). However, if you wish to request 3D Secure based on logic from your own fraud engine, provide this option. Read our guide on [manually requesting 3D Secure](https://stripe.com/docs/payments/3d-secure/authentication-flow#manual-three-ds) for more information on how this configuration interacts with Radar and our SCA Engine.
        """

    class UpdateParamsPaymentSettingsPaymentMethodOptionsCardInstallments(
        TypedDict,
    ):
        enabled: NotRequired[bool]
        """
        Setting to true enables installments for this invoice.
        Setting to false will prevent any selected plan from applying to a payment.
        """
        plan: NotRequired[
            "Literal['']|InvoiceService.UpdateParamsPaymentSettingsPaymentMethodOptionsCardInstallmentsPlan"
        ]
        """
        The selected installment plan to use for this invoice.
        """

    class UpdateParamsPaymentSettingsPaymentMethodOptionsCardInstallmentsPlan(
        TypedDict,
    ):
        count: NotRequired[int]
        """
        For `fixed_count` installment plans, this is required. It represents the number of installment payments your customer will make to their credit card.
        """
        interval: NotRequired[Literal["month"]]
        """
        For `fixed_count` installment plans, this is required. It represents the interval between installment payments your customer will make to their credit card.
        One of `month`.
        """
        type: Literal["fixed_count"]
        """
        Type of installment plan, one of `fixed_count`.
        """

    class UpdateParamsPaymentSettingsPaymentMethodOptionsCustomerBalance(
        TypedDict,
    ):
        bank_transfer: NotRequired[
            "InvoiceService.UpdateParamsPaymentSettingsPaymentMethodOptionsCustomerBalanceBankTransfer"
        ]
        """
        Configuration for the bank transfer funding type, if the `funding_type` is set to `bank_transfer`.
        """
        funding_type: NotRequired[str]
        """
        The funding method type to be used when there are not enough funds in the customer balance. Permitted values include: `bank_transfer`.
        """

    class UpdateParamsPaymentSettingsPaymentMethodOptionsCustomerBalanceBankTransfer(
        TypedDict,
    ):
        eu_bank_transfer: NotRequired[
            "InvoiceService.UpdateParamsPaymentSettingsPaymentMethodOptionsCustomerBalanceBankTransferEuBankTransfer"
        ]
        """
        Configuration for eu_bank_transfer funding type.
        """
        type: NotRequired[str]
        """
        The bank transfer type that can be used for funding. Permitted values include: `eu_bank_transfer`, `gb_bank_transfer`, `jp_bank_transfer`, `mx_bank_transfer`, or `us_bank_transfer`.
        """

    class UpdateParamsPaymentSettingsPaymentMethodOptionsCustomerBalanceBankTransferEuBankTransfer(
        TypedDict,
    ):
        country: str
        """
        The desired country code of the bank account information. Permitted values include: `BE`, `DE`, `ES`, `FR`, `IE`, or `NL`.
        """

    class UpdateParamsPaymentSettingsPaymentMethodOptionsKonbini(TypedDict):
        pass

    class UpdateParamsPaymentSettingsPaymentMethodOptionsSepaDebit(TypedDict):
        pass

    class UpdateParamsPaymentSettingsPaymentMethodOptionsUsBankAccount(
        TypedDict,
    ):
        financial_connections: NotRequired[
            "InvoiceService.UpdateParamsPaymentSettingsPaymentMethodOptionsUsBankAccountFinancialConnections"
        ]
        """
        Additional fields for Financial Connections Session creation
        """
        verification_method: NotRequired[
            Literal["automatic", "instant", "microdeposits"]
        ]
        """
        Verification method for the intent
        """

    class UpdateParamsPaymentSettingsPaymentMethodOptionsUsBankAccountFinancialConnections(
        TypedDict,
    ):
        filters: NotRequired[
            "InvoiceService.UpdateParamsPaymentSettingsPaymentMethodOptionsUsBankAccountFinancialConnectionsFilters"
        ]
        """
        Provide filters for the linked accounts that the customer can select for the payment method.
        """
        permissions: NotRequired[
            List[
                Literal[
                    "balances", "ownership", "payment_method", "transactions"
                ]
            ]
        ]
        """
        The list of permissions to request. If this parameter is passed, the `payment_method` permission must be included. Valid permissions include: `balances`, `ownership`, `payment_method`, and `transactions`.
        """
        prefetch: NotRequired[
            List[Literal["balances", "ownership", "transactions"]]
        ]
        """
        List of data features that you would like to retrieve upon account creation.
        """

    class UpdateParamsPaymentSettingsPaymentMethodOptionsUsBankAccountFinancialConnectionsFilters(
        TypedDict,
    ):
        account_subcategories: NotRequired[
            List[Literal["checking", "savings"]]
        ]
        """
        The account subcategories to use to filter for selectable accounts. Valid subcategories are `checking` and `savings`.
        """

    class UpdateParamsRendering(TypedDict):
        amount_tax_display: NotRequired[
            "Literal['']|Literal['exclude_tax', 'include_inclusive_tax']"
        ]
        """
        How line-item prices and amounts will be displayed with respect to tax on invoice PDFs. One of `exclude_tax` or `include_inclusive_tax`. `include_inclusive_tax` will include inclusive tax (and exclude exclusive tax) in invoice PDF amounts. `exclude_tax` will exclude all tax (inclusive and exclusive alike) from invoice PDF amounts.
        """
        pdf: NotRequired["InvoiceService.UpdateParamsRenderingPdf"]
        """
        Invoice pdf rendering options
        """

    class UpdateParamsRenderingPdf(TypedDict):
        page_size: NotRequired[Literal["a4", "auto", "letter"]]
        """
        Page size for invoice PDF. Can be set to `a4`, `letter`, or `auto`.
         If set to `auto`, invoice PDF page size defaults to `a4` for customers with
         Japanese locale and `letter` for customers with other locales.
        """

    class UpdateParamsShippingCost(TypedDict):
        shipping_rate: NotRequired[str]
        """
        The ID of the shipping rate to use for this order.
        """
        shipping_rate_data: NotRequired[
            "InvoiceService.UpdateParamsShippingCostShippingRateData"
        ]
        """
        Parameters to create a new ad-hoc shipping rate for this order.
        """

    class UpdateParamsShippingCostShippingRateData(TypedDict):
        delivery_estimate: NotRequired[
            "InvoiceService.UpdateParamsShippingCostShippingRateDataDeliveryEstimate"
        ]
        """
        The estimated range for how long shipping will take, meant to be displayable to the customer. This will appear on CheckoutSessions.
        """
        display_name: str
        """
        The name of the shipping rate, meant to be displayable to the customer. This will appear on CheckoutSessions.
        """
        fixed_amount: NotRequired[
            "InvoiceService.UpdateParamsShippingCostShippingRateDataFixedAmount"
        ]
        """
        Describes a fixed amount to charge for shipping. Must be present if type is `fixed_amount`.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        tax_behavior: NotRequired[
            Literal["exclusive", "inclusive", "unspecified"]
        ]
        """
        Specifies whether the rate is considered inclusive of taxes or exclusive of taxes. One of `inclusive`, `exclusive`, or `unspecified`.
        """
        tax_code: NotRequired[str]
        """
        A [tax code](https://stripe.com/docs/tax/tax-categories) ID. The Shipping tax code is `txcd_92010001`.
        """
        type: NotRequired[Literal["fixed_amount"]]
        """
        The type of calculation to use on the shipping rate.
        """

    class UpdateParamsShippingCostShippingRateDataDeliveryEstimate(TypedDict):
        maximum: NotRequired[
            "InvoiceService.UpdateParamsShippingCostShippingRateDataDeliveryEstimateMaximum"
        ]
        """
        The upper bound of the estimated range. If empty, represents no upper bound i.e., infinite.
        """
        minimum: NotRequired[
            "InvoiceService.UpdateParamsShippingCostShippingRateDataDeliveryEstimateMinimum"
        ]
        """
        The lower bound of the estimated range. If empty, represents no lower bound.
        """

    class UpdateParamsShippingCostShippingRateDataDeliveryEstimateMaximum(
        TypedDict,
    ):
        unit: Literal["business_day", "day", "hour", "month", "week"]
        """
        A unit of time.
        """
        value: int
        """
        Must be greater than 0.
        """

    class UpdateParamsShippingCostShippingRateDataDeliveryEstimateMinimum(
        TypedDict,
    ):
        unit: Literal["business_day", "day", "hour", "month", "week"]
        """
        A unit of time.
        """
        value: int
        """
        Must be greater than 0.
        """

    class UpdateParamsShippingCostShippingRateDataFixedAmount(TypedDict):
        amount: int
        """
        A non-negative integer in cents representing how much to charge.
        """
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        currency_options: NotRequired[
            Dict[
                str,
                "InvoiceService.UpdateParamsShippingCostShippingRateDataFixedAmountCurrencyOptions",
            ]
        ]
        """
        Shipping rates defined in each available currency option. Each key must be a three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html) and a [supported currency](https://stripe.com/docs/currencies).
        """

    class UpdateParamsShippingCostShippingRateDataFixedAmountCurrencyOptions(
        TypedDict,
    ):
        amount: int
        """
        A non-negative integer in cents representing how much to charge.
        """
        tax_behavior: NotRequired[
            Literal["exclusive", "inclusive", "unspecified"]
        ]
        """
        Specifies whether the rate is considered inclusive of taxes or exclusive of taxes. One of `inclusive`, `exclusive`, or `unspecified`.
        """

    class UpdateParamsShippingDetails(TypedDict):
        address: "InvoiceService.UpdateParamsShippingDetailsAddress"
        """
        Shipping address
        """
        name: str
        """
        Recipient name.
        """
        phone: NotRequired["Literal['']|str"]
        """
        Recipient phone (including extension)
        """

    class UpdateParamsShippingDetailsAddress(TypedDict):
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

    class UpdateParamsTransferData(TypedDict):
        amount: NotRequired[int]
        """
        The amount that will be transferred automatically when the invoice is paid. If no amount is set, the full amount is transferred.
        """
        destination: str
        """
        ID of an existing, connected Stripe account.
        """

    class VoidInvoiceParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    def delete(
        self,
        invoice: str,
        params: "InvoiceService.DeleteParams" = {},
        options: RequestOptions = {},
    ) -> Invoice:
        """
        Permanently deletes a one-off invoice draft. This cannot be undone. Attempts to delete invoices that are no longer in a draft state will fail; once an invoice has been finalized or if an invoice is for a subscription, it must be [voided](https://stripe.com/docs/api#void_invoice).
        """
        return cast(
            Invoice,
            self._request(
                "delete",
                "/v1/invoices/{invoice}".format(invoice=sanitize_id(invoice)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def delete_async(
        self,
        invoice: str,
        params: "InvoiceService.DeleteParams" = {},
        options: RequestOptions = {},
    ) -> Invoice:
        """
        Permanently deletes a one-off invoice draft. This cannot be undone. Attempts to delete invoices that are no longer in a draft state will fail; once an invoice has been finalized or if an invoice is for a subscription, it must be [voided](https://stripe.com/docs/api#void_invoice).
        """
        return cast(
            Invoice,
            await self._request_async(
                "delete",
                "/v1/invoices/{invoice}".format(invoice=sanitize_id(invoice)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        invoice: str,
        params: "InvoiceService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Invoice:
        """
        Retrieves the invoice with the given ID.
        """
        return cast(
            Invoice,
            self._request(
                "get",
                "/v1/invoices/{invoice}".format(invoice=sanitize_id(invoice)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        invoice: str,
        params: "InvoiceService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Invoice:
        """
        Retrieves the invoice with the given ID.
        """
        return cast(
            Invoice,
            await self._request_async(
                "get",
                "/v1/invoices/{invoice}".format(invoice=sanitize_id(invoice)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def update(
        self,
        invoice: str,
        params: "InvoiceService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Invoice:
        """
        Draft invoices are fully editable. Once an invoice is [finalized](https://stripe.com/docs/billing/invoices/workflow#finalized),
        monetary values, as well as collection_method, become uneditable.

        If you would like to stop the Stripe Billing engine from automatically finalizing, reattempting payments on,
        sending reminders for, or [automatically reconciling](https://stripe.com/docs/billing/invoices/reconciliation) invoices, pass
        auto_advance=false.
        """
        return cast(
            Invoice,
            self._request(
                "post",
                "/v1/invoices/{invoice}".format(invoice=sanitize_id(invoice)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def update_async(
        self,
        invoice: str,
        params: "InvoiceService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Invoice:
        """
        Draft invoices are fully editable. Once an invoice is [finalized](https://stripe.com/docs/billing/invoices/workflow#finalized),
        monetary values, as well as collection_method, become uneditable.

        If you would like to stop the Stripe Billing engine from automatically finalizing, reattempting payments on,
        sending reminders for, or [automatically reconciling](https://stripe.com/docs/billing/invoices/reconciliation) invoices, pass
        auto_advance=false.
        """
        return cast(
            Invoice,
            await self._request_async(
                "post",
                "/v1/invoices/{invoice}".format(invoice=sanitize_id(invoice)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def list(
        self,
        params: "InvoiceService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Invoice]:
        """
        You can list all invoices, or list the invoices for a specific customer. The invoices are returned sorted by creation date, with the most recently created invoices appearing first.
        """
        return cast(
            ListObject[Invoice],
            self._request(
                "get",
                "/v1/invoices",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "InvoiceService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Invoice]:
        """
        You can list all invoices, or list the invoices for a specific customer. The invoices are returned sorted by creation date, with the most recently created invoices appearing first.
        """
        return cast(
            ListObject[Invoice],
            await self._request_async(
                "get",
                "/v1/invoices",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self,
        params: "InvoiceService.CreateParams" = {},
        options: RequestOptions = {},
    ) -> Invoice:
        """
        This endpoint creates a draft invoice for a given customer. The invoice remains a draft until you [finalize the invoice, which allows you to [pay](#pay_invoice) or <a href="#send_invoice">send](https://stripe.com/docs/api#finalize_invoice) the invoice to your customers.
        """
        return cast(
            Invoice,
            self._request(
                "post",
                "/v1/invoices",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        params: "InvoiceService.CreateParams" = {},
        options: RequestOptions = {},
    ) -> Invoice:
        """
        This endpoint creates a draft invoice for a given customer. The invoice remains a draft until you [finalize the invoice, which allows you to [pay](#pay_invoice) or <a href="#send_invoice">send](https://stripe.com/docs/api#finalize_invoice) the invoice to your customers.
        """
        return cast(
            Invoice,
            await self._request_async(
                "post",
                "/v1/invoices",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def search(
        self,
        params: "InvoiceService.SearchParams",
        options: RequestOptions = {},
    ) -> SearchResultObject[Invoice]:
        """
        Search for invoices you've previously created using Stripe's [Search Query Language](https://stripe.com/docs/search#search-query-language).
        Don't use search in read-after-write flows where strict consistency is necessary. Under normal operating
        conditions, data is searchable in less than a minute. Occasionally, propagation of new or updated data can be up
        to an hour behind during outages. Search functionality is not available to merchants in India.
        """
        return cast(
            SearchResultObject[Invoice],
            self._request(
                "get",
                "/v1/invoices/search",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def search_async(
        self,
        params: "InvoiceService.SearchParams",
        options: RequestOptions = {},
    ) -> SearchResultObject[Invoice]:
        """
        Search for invoices you've previously created using Stripe's [Search Query Language](https://stripe.com/docs/search#search-query-language).
        Don't use search in read-after-write flows where strict consistency is necessary. Under normal operating
        conditions, data is searchable in less than a minute. Occasionally, propagation of new or updated data can be up
        to an hour behind during outages. Search functionality is not available to merchants in India.
        """
        return cast(
            SearchResultObject[Invoice],
            await self._request_async(
                "get",
                "/v1/invoices/search",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def upcoming(
        self,
        params: "InvoiceService.UpcomingParams" = {},
        options: RequestOptions = {},
    ) -> Invoice:
        """
        At any time, you can preview the upcoming invoice for a customer. This will show you all the charges that are pending, including subscription renewal charges, invoice item charges, etc. It will also show you any discounts that are applicable to the invoice.

        Note that when you are viewing an upcoming invoice, you are simply viewing a preview – the invoice has not yet been created. As such, the upcoming invoice will not show up in invoice listing calls, and you cannot use the API to pay or edit the invoice. If you want to change the amount that your customer will be billed, you can add, remove, or update pending invoice items, or update the customer's discount.

        You can preview the effects of updating a subscription, including a preview of what proration will take place. To ensure that the actual proration is calculated exactly the same as the previewed proration, you should pass the subscription_details.proration_date parameter when doing the actual subscription update. The recommended way to get only the prorations being previewed is to consider only proration line items where period[start] is equal to the subscription_details.proration_date value passed in the request.

        Note: Currency conversion calculations use the latest exchange rates. Exchange rates may vary between the time of the preview and the time of the actual invoice creation. [Learn more](https://docs.stripe.com/currencies/conversions)
        """
        return cast(
            Invoice,
            self._request(
                "get",
                "/v1/invoices/upcoming",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def upcoming_async(
        self,
        params: "InvoiceService.UpcomingParams" = {},
        options: RequestOptions = {},
    ) -> Invoice:
        """
        At any time, you can preview the upcoming invoice for a customer. This will show you all the charges that are pending, including subscription renewal charges, invoice item charges, etc. It will also show you any discounts that are applicable to the invoice.

        Note that when you are viewing an upcoming invoice, you are simply viewing a preview – the invoice has not yet been created. As such, the upcoming invoice will not show up in invoice listing calls, and you cannot use the API to pay or edit the invoice. If you want to change the amount that your customer will be billed, you can add, remove, or update pending invoice items, or update the customer's discount.

        You can preview the effects of updating a subscription, including a preview of what proration will take place. To ensure that the actual proration is calculated exactly the same as the previewed proration, you should pass the subscription_details.proration_date parameter when doing the actual subscription update. The recommended way to get only the prorations being previewed is to consider only proration line items where period[start] is equal to the subscription_details.proration_date value passed in the request.

        Note: Currency conversion calculations use the latest exchange rates. Exchange rates may vary between the time of the preview and the time of the actual invoice creation. [Learn more](https://docs.stripe.com/currencies/conversions)
        """
        return cast(
            Invoice,
            await self._request_async(
                "get",
                "/v1/invoices/upcoming",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def add_lines(
        self,
        invoice: str,
        params: "InvoiceService.AddLinesParams",
        options: RequestOptions = {},
    ) -> Invoice:
        """
        Adds multiple line items to an invoice. This is only possible when an invoice is still a draft.
        """
        return cast(
            Invoice,
            self._request(
                "post",
                "/v1/invoices/{invoice}/add_lines".format(
                    invoice=sanitize_id(invoice),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def add_lines_async(
        self,
        invoice: str,
        params: "InvoiceService.AddLinesParams",
        options: RequestOptions = {},
    ) -> Invoice:
        """
        Adds multiple line items to an invoice. This is only possible when an invoice is still a draft.
        """
        return cast(
            Invoice,
            await self._request_async(
                "post",
                "/v1/invoices/{invoice}/add_lines".format(
                    invoice=sanitize_id(invoice),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def finalize_invoice(
        self,
        invoice: str,
        params: "InvoiceService.FinalizeInvoiceParams" = {},
        options: RequestOptions = {},
    ) -> Invoice:
        """
        Stripe automatically finalizes drafts before sending and attempting payment on invoices. However, if you'd like to finalize a draft invoice manually, you can do so using this method.
        """
        return cast(
            Invoice,
            self._request(
                "post",
                "/v1/invoices/{invoice}/finalize".format(
                    invoice=sanitize_id(invoice),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def finalize_invoice_async(
        self,
        invoice: str,
        params: "InvoiceService.FinalizeInvoiceParams" = {},
        options: RequestOptions = {},
    ) -> Invoice:
        """
        Stripe automatically finalizes drafts before sending and attempting payment on invoices. However, if you'd like to finalize a draft invoice manually, you can do so using this method.
        """
        return cast(
            Invoice,
            await self._request_async(
                "post",
                "/v1/invoices/{invoice}/finalize".format(
                    invoice=sanitize_id(invoice),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def mark_uncollectible(
        self,
        invoice: str,
        params: "InvoiceService.MarkUncollectibleParams" = {},
        options: RequestOptions = {},
    ) -> Invoice:
        """
        Marking an invoice as uncollectible is useful for keeping track of bad debts that can be written off for accounting purposes.
        """
        return cast(
            Invoice,
            self._request(
                "post",
                "/v1/invoices/{invoice}/mark_uncollectible".format(
                    invoice=sanitize_id(invoice),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def mark_uncollectible_async(
        self,
        invoice: str,
        params: "InvoiceService.MarkUncollectibleParams" = {},
        options: RequestOptions = {},
    ) -> Invoice:
        """
        Marking an invoice as uncollectible is useful for keeping track of bad debts that can be written off for accounting purposes.
        """
        return cast(
            Invoice,
            await self._request_async(
                "post",
                "/v1/invoices/{invoice}/mark_uncollectible".format(
                    invoice=sanitize_id(invoice),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def pay(
        self,
        invoice: str,
        params: "InvoiceService.PayParams" = {},
        options: RequestOptions = {},
    ) -> Invoice:
        """
        Stripe automatically creates and then attempts to collect payment on invoices for customers on subscriptions according to your [subscriptions settings](https://dashboard.stripe.com/account/billing/automatic). However, if you'd like to attempt payment on an invoice out of the normal collection schedule or for some other reason, you can do so.
        """
        return cast(
            Invoice,
            self._request(
                "post",
                "/v1/invoices/{invoice}/pay".format(
                    invoice=sanitize_id(invoice),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def pay_async(
        self,
        invoice: str,
        params: "InvoiceService.PayParams" = {},
        options: RequestOptions = {},
    ) -> Invoice:
        """
        Stripe automatically creates and then attempts to collect payment on invoices for customers on subscriptions according to your [subscriptions settings](https://dashboard.stripe.com/account/billing/automatic). However, if you'd like to attempt payment on an invoice out of the normal collection schedule or for some other reason, you can do so.
        """
        return cast(
            Invoice,
            await self._request_async(
                "post",
                "/v1/invoices/{invoice}/pay".format(
                    invoice=sanitize_id(invoice),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def remove_lines(
        self,
        invoice: str,
        params: "InvoiceService.RemoveLinesParams",
        options: RequestOptions = {},
    ) -> Invoice:
        """
        Removes multiple line items from an invoice. This is only possible when an invoice is still a draft.
        """
        return cast(
            Invoice,
            self._request(
                "post",
                "/v1/invoices/{invoice}/remove_lines".format(
                    invoice=sanitize_id(invoice),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def remove_lines_async(
        self,
        invoice: str,
        params: "InvoiceService.RemoveLinesParams",
        options: RequestOptions = {},
    ) -> Invoice:
        """
        Removes multiple line items from an invoice. This is only possible when an invoice is still a draft.
        """
        return cast(
            Invoice,
            await self._request_async(
                "post",
                "/v1/invoices/{invoice}/remove_lines".format(
                    invoice=sanitize_id(invoice),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def send_invoice(
        self,
        invoice: str,
        params: "InvoiceService.SendInvoiceParams" = {},
        options: RequestOptions = {},
    ) -> Invoice:
        """
        Stripe will automatically send invoices to customers according to your [subscriptions settings](https://dashboard.stripe.com/account/billing/automatic). However, if you'd like to manually send an invoice to your customer out of the normal schedule, you can do so. When sending invoices that have already been paid, there will be no reference to the payment in the email.

        Requests made in test-mode result in no emails being sent, despite sending an invoice.sent event.
        """
        return cast(
            Invoice,
            self._request(
                "post",
                "/v1/invoices/{invoice}/send".format(
                    invoice=sanitize_id(invoice),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def send_invoice_async(
        self,
        invoice: str,
        params: "InvoiceService.SendInvoiceParams" = {},
        options: RequestOptions = {},
    ) -> Invoice:
        """
        Stripe will automatically send invoices to customers according to your [subscriptions settings](https://dashboard.stripe.com/account/billing/automatic). However, if you'd like to manually send an invoice to your customer out of the normal schedule, you can do so. When sending invoices that have already been paid, there will be no reference to the payment in the email.

        Requests made in test-mode result in no emails being sent, despite sending an invoice.sent event.
        """
        return cast(
            Invoice,
            await self._request_async(
                "post",
                "/v1/invoices/{invoice}/send".format(
                    invoice=sanitize_id(invoice),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def update_lines(
        self,
        invoice: str,
        params: "InvoiceService.UpdateLinesParams",
        options: RequestOptions = {},
    ) -> Invoice:
        """
        Updates multiple line items on an invoice. This is only possible when an invoice is still a draft.
        """
        return cast(
            Invoice,
            self._request(
                "post",
                "/v1/invoices/{invoice}/update_lines".format(
                    invoice=sanitize_id(invoice),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def update_lines_async(
        self,
        invoice: str,
        params: "InvoiceService.UpdateLinesParams",
        options: RequestOptions = {},
    ) -> Invoice:
        """
        Updates multiple line items on an invoice. This is only possible when an invoice is still a draft.
        """
        return cast(
            Invoice,
            await self._request_async(
                "post",
                "/v1/invoices/{invoice}/update_lines".format(
                    invoice=sanitize_id(invoice),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def void_invoice(
        self,
        invoice: str,
        params: "InvoiceService.VoidInvoiceParams" = {},
        options: RequestOptions = {},
    ) -> Invoice:
        """
        Mark a finalized invoice as void. This cannot be undone. Voiding an invoice is similar to [deletion](https://stripe.com/docs/api#delete_invoice), however it only applies to finalized invoices and maintains a papertrail where the invoice can still be found.

        Consult with local regulations to determine whether and how an invoice might be amended, canceled, or voided in the jurisdiction you're doing business in. You might need to [issue another invoice or <a href="#create_credit_note">credit note](https://stripe.com/docs/api#create_invoice) instead. Stripe recommends that you consult with your legal counsel for advice specific to your business.
        """
        return cast(
            Invoice,
            self._request(
                "post",
                "/v1/invoices/{invoice}/void".format(
                    invoice=sanitize_id(invoice),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def void_invoice_async(
        self,
        invoice: str,
        params: "InvoiceService.VoidInvoiceParams" = {},
        options: RequestOptions = {},
    ) -> Invoice:
        """
        Mark a finalized invoice as void. This cannot be undone. Voiding an invoice is similar to [deletion](https://stripe.com/docs/api#delete_invoice), however it only applies to finalized invoices and maintains a papertrail where the invoice can still be found.

        Consult with local regulations to determine whether and how an invoice might be amended, canceled, or voided in the jurisdiction you're doing business in. You might need to [issue another invoice or <a href="#create_credit_note">credit note](https://stripe.com/docs/api#create_invoice) instead. Stripe recommends that you consult with your legal counsel for advice specific to your business.
        """
        return cast(
            Invoice,
            await self._request_async(
                "post",
                "/v1/invoices/{invoice}/void".format(
                    invoice=sanitize_id(invoice),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create_preview(
        self,
        params: "InvoiceService.CreatePreviewParams" = {},
        options: RequestOptions = {},
    ) -> Invoice:
        """
        At any time, you can preview the upcoming invoice for a customer. This will show you all the charges that are pending, including subscription renewal charges, invoice item charges, etc. It will also show you any discounts that are applicable to the invoice.

        Note that when you are viewing an upcoming invoice, you are simply viewing a preview – the invoice has not yet been created. As such, the upcoming invoice will not show up in invoice listing calls, and you cannot use the API to pay or edit the invoice. If you want to change the amount that your customer will be billed, you can add, remove, or update pending invoice items, or update the customer's discount.

        You can preview the effects of updating a subscription, including a preview of what proration will take place. To ensure that the actual proration is calculated exactly the same as the previewed proration, you should pass the subscription_details.proration_date parameter when doing the actual subscription update. The recommended way to get only the prorations being previewed is to consider only proration line items where period[start] is equal to the subscription_details.proration_date value passed in the request.

        Note: Currency conversion calculations use the latest exchange rates. Exchange rates may vary between the time of the preview and the time of the actual invoice creation. [Learn more](https://docs.stripe.com/currencies/conversions)
        """
        return cast(
            Invoice,
            self._request(
                "post",
                "/v1/invoices/create_preview",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_preview_async(
        self,
        params: "InvoiceService.CreatePreviewParams" = {},
        options: RequestOptions = {},
    ) -> Invoice:
        """
        At any time, you can preview the upcoming invoice for a customer. This will show you all the charges that are pending, including subscription renewal charges, invoice item charges, etc. It will also show you any discounts that are applicable to the invoice.

        Note that when you are viewing an upcoming invoice, you are simply viewing a preview – the invoice has not yet been created. As such, the upcoming invoice will not show up in invoice listing calls, and you cannot use the API to pay or edit the invoice. If you want to change the amount that your customer will be billed, you can add, remove, or update pending invoice items, or update the customer's discount.

        You can preview the effects of updating a subscription, including a preview of what proration will take place. To ensure that the actual proration is calculated exactly the same as the previewed proration, you should pass the subscription_details.proration_date parameter when doing the actual subscription update. The recommended way to get only the prorations being previewed is to consider only proration line items where period[start] is equal to the subscription_details.proration_date value passed in the request.

        Note: Currency conversion calculations use the latest exchange rates. Exchange rates may vary between the time of the preview and the time of the actual invoice creation. [Learn more](https://docs.stripe.com/currencies/conversions)
        """
        return cast(
            Invoice,
            await self._request_async(
                "post",
                "/v1/invoices/create_preview",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

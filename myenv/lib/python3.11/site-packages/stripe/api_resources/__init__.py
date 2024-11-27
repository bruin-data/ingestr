# The beginning of the section generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources package is deprecated, please change your
    imports to import from stripe directly.
    From:
      from stripe.api_resources import ...
    To:
      from stripe import ...
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe.api_resources import (
        abstract,
        apps,
        billing,
        billing_portal,
        checkout,
        climate,
        entitlements,
        financial_connections,
        forwarding,
        identity,
        issuing,
        radar,
        reporting,
        sigma,
        tax,
        terminal,
        test_helpers,
        treasury,
    )
    from stripe.api_resources.account import Account
    from stripe.api_resources.account_link import AccountLink
    from stripe.api_resources.account_session import AccountSession
    from stripe.api_resources.apple_pay_domain import ApplePayDomain
    from stripe.api_resources.application import Application
    from stripe.api_resources.application_fee import ApplicationFee
    from stripe.api_resources.application_fee_refund import (
        ApplicationFeeRefund,
    )
    from stripe.api_resources.balance import Balance
    from stripe.api_resources.balance_transaction import BalanceTransaction
    from stripe.api_resources.bank_account import BankAccount
    from stripe.api_resources.capability import Capability
    from stripe.api_resources.card import Card
    from stripe.api_resources.cash_balance import CashBalance
    from stripe.api_resources.charge import Charge
    from stripe.api_resources.confirmation_token import ConfirmationToken
    from stripe.api_resources.connect_collection_transfer import (
        ConnectCollectionTransfer,
    )
    from stripe.api_resources.country_spec import CountrySpec
    from stripe.api_resources.coupon import Coupon
    from stripe.api_resources.credit_note import CreditNote
    from stripe.api_resources.credit_note_line_item import CreditNoteLineItem
    from stripe.api_resources.customer import Customer
    from stripe.api_resources.customer_balance_transaction import (
        CustomerBalanceTransaction,
    )
    from stripe.api_resources.customer_cash_balance_transaction import (
        CustomerCashBalanceTransaction,
    )
    from stripe.api_resources.customer_session import CustomerSession
    from stripe.api_resources.discount import Discount
    from stripe.api_resources.dispute import Dispute
    from stripe.api_resources.ephemeral_key import EphemeralKey
    from stripe.api_resources.event import Event
    from stripe.api_resources.exchange_rate import ExchangeRate
    from stripe.api_resources.file import File
    from stripe.api_resources.file_link import FileLink
    from stripe.api_resources.funding_instructions import FundingInstructions
    from stripe.api_resources.invoice import Invoice
    from stripe.api_resources.invoice_item import InvoiceItem
    from stripe.api_resources.invoice_line_item import InvoiceLineItem
    from stripe.api_resources.line_item import LineItem
    from stripe.api_resources.list_object import ListObject
    from stripe.api_resources.login_link import LoginLink
    from stripe.api_resources.mandate import Mandate
    from stripe.api_resources.payment_intent import PaymentIntent
    from stripe.api_resources.payment_link import PaymentLink
    from stripe.api_resources.payment_method import PaymentMethod
    from stripe.api_resources.payment_method_configuration import (
        PaymentMethodConfiguration,
    )
    from stripe.api_resources.payment_method_domain import PaymentMethodDomain
    from stripe.api_resources.payout import Payout
    from stripe.api_resources.person import Person
    from stripe.api_resources.plan import Plan
    from stripe.api_resources.price import Price
    from stripe.api_resources.product import Product
    from stripe.api_resources.product_feature import ProductFeature
    from stripe.api_resources.promotion_code import PromotionCode
    from stripe.api_resources.quote import Quote
    from stripe.api_resources.refund import Refund
    from stripe.api_resources.reserve_transaction import ReserveTransaction
    from stripe.api_resources.reversal import Reversal
    from stripe.api_resources.review import Review
    from stripe.api_resources.search_result_object import SearchResultObject
    from stripe.api_resources.setup_attempt import SetupAttempt
    from stripe.api_resources.setup_intent import SetupIntent
    from stripe.api_resources.shipping_rate import ShippingRate
    from stripe.api_resources.source import Source
    from stripe.api_resources.source_mandate_notification import (
        SourceMandateNotification,
    )
    from stripe.api_resources.source_transaction import SourceTransaction
    from stripe.api_resources.subscription import Subscription
    from stripe.api_resources.subscription_item import SubscriptionItem
    from stripe.api_resources.subscription_schedule import SubscriptionSchedule
    from stripe.api_resources.tax_code import TaxCode
    from stripe.api_resources.tax_deducted_at_source import TaxDeductedAtSource
    from stripe.api_resources.tax_id import TaxId
    from stripe.api_resources.tax_rate import TaxRate
    from stripe.api_resources.token import Token
    from stripe.api_resources.topup import Topup
    from stripe.api_resources.transfer import Transfer
    from stripe.api_resources.usage_record import UsageRecord
    from stripe.api_resources.usage_record_summary import UsageRecordSummary
    from stripe.api_resources.webhook_endpoint import WebhookEndpoint

# The end of the section generated from our OpenAPI spec

# These two exports are "special" and can't be handled by the generator.
#   - error_object exports two symbols
#   - File is renamed to FileUpload on export
#   - RecipientTransfer is a manually maintained deprecated class
if not TYPE_CHECKING:
    from stripe.api_resources.error_object import ErrorObject
    from stripe.api_resources.error_object import (
        OAuthErrorObject,
    )
    from stripe.api_resources.file import (
        File as FileUpload,
    )

    from stripe.api_resources.recipient_transfer import RecipientTransfer

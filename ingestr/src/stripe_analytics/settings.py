# Copyright 2022-2025 ScaleVector
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#   http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""Stripe analytics source settings and constants"""

# the most popular endpoints
# Full list of the Stripe API endpoints you can find here: https://stripe.com/docs/api.
ENDPOINTS = {
    "account": "Account",
    "applepaydomain": "ApplePayDomain",
    "apple_pay_domain": "ApplePayDomain",
    "applicationfee": "ApplicationFee",
    "application_fee": "ApplicationFee",
    "checkoutsession": "CheckoutSession",
    "checkout_session": "CheckoutSession",
    "coupon": "Coupon",
    "charge": "Charge",
    "customer": "Customer",
    "dispute": "Dispute",
    "paymentintent": "PaymentIntent",
    "payment_intent": "PaymentIntent",
    "paymentlink": "PaymentLink",
    "payment_link": "PaymentLink",
    "paymentmethod": "PaymentMethod",
    "payment_method": "PaymentMethod",
    "paymentmethoddomain": "PaymentMethodDomain",
    "payment_method_domain": "PaymentMethodDomain",
    "payout": "Payout",
    "plan": "Plan",
    "price": "Price",
    "product": "Product",
    "promotioncode": "PromotionCode",
    "promotion_code": "PromotionCode",
    "quote": "Quote",
    "refund": "Refund",
    "review": "Review",
    "setupattempt": "SetupAttempt",
    "setup_attempt": "SetupAttempt",
    "setupintent": "SetupIntent",
    "setup_intent": "SetupIntent",
    "shippingrate": "ShippingRate",
    "shipping_rate": "ShippingRate",
    "subscription": "Subscription",
    "subscriptionitem": "SubscriptionItem",
    "subscription_item": "SubscriptionItem",
    "subscriptionschedule": "SubscriptionSchedule",
    "subscription_schedule": "SubscriptionSchedule",
    "transfer": "Transfer",
    "taxcode": "TaxCode",
    "tax_code": "TaxCode",
    "taxid": "TaxId",
    "tax_id": "TaxId",
    "taxrate": "TaxRate",
    "tax_rate": "TaxRate",
    "topup": "Topup",
    "top_up": "Topup",
    "webhookendpoint": "WebhookEndpoint",
    "webhook_endpoint": "WebhookEndpoint",
    "invoice": "Invoice",
    "invoiceitem": "InvoiceItem",
    "invoice_item": "InvoiceItem",
    "invoicelineitem": "InvoiceLineItem",
    "invoice_line_item": "InvoiceLineItem",
    "balancetransaction": "BalanceTransaction",
    "balance_transaction": "BalanceTransaction",
    "creditnote": "CreditNote",
    "credit_note": "CreditNote",
    "event": "Event",
}

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
}
# possible incremental endpoints
INCREMENTAL_ENDPOINTS = {
    "applicationfee": "ApplicationFee",
    "application_fee": "ApplicationFee",
    "balancetransaction": "BalanceTransaction",
    "balance_transaction": "BalanceTransaction",
    "charge": "Charge",
    "creditnote": "CreditNote",
    "credit_note": "CreditNote",
    "event": "Event",
    "invoice": "Invoice",
    "invoiceitem": "InvoiceItem",
    "invoice_item": "InvoiceItem",
    "invoicelineitem": "InvoiceLineItem",
    "invoice_line_item": "InvoiceLineItem",
    "setupattempt": "SetupAttempt",
    "setup_attempt": "SetupAttempt",
}

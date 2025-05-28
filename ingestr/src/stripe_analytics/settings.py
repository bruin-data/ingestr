"""Stripe analytics source settings and constants"""

# the most popular endpoints
# Full list of the Stripe API endpoints you can find here: https://stripe.com/docs/api.
ENDPOINTS = {
    "subscription": "Subscription",
    "account": "Account",
    "coupon": "Coupon",
    "customer": "Customer",
    "product": "Product",
    "price": "Price",
    "shippingrate": "ShippingRate",
    "dispute": "Dispute",
    "subscriptionitem": "SubscriptionItem",
    "checkoutsession": "CheckoutSession",
}
# possible incremental endpoints
INCREMENTAL_ENDPOINTS = {
    "event": "Event",
    "invoice": "Invoice",
    "balancetransaction": "BalanceTransaction",
    "charge": "Charge",
    "applicationfee": "ApplicationFee",
    "setupattempt": "SetupAttempt",
    "creditnote": "CreditNote",
}

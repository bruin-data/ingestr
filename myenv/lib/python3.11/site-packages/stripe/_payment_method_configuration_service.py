# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._payment_method_configuration import PaymentMethodConfiguration
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from typing import List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class PaymentMethodConfigurationService(StripeService):
    class CreateParams(TypedDict):
        acss_debit: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsAcssDebit"
        ]
        """
        Canadian pre-authorized debit payments, check this [page](https://stripe.com/docs/payments/acss-debit) for more details like country availability.
        """
        affirm: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsAffirm"
        ]
        """
        [Affirm](https://www.affirm.com/) gives your customers a way to split purchases over a series of payments. Depending on the purchase, they can pay with four interest-free payments (Split Pay) or pay over a longer term (Installments), which might include interest. Check this [page](https://stripe.com/docs/payments/affirm) for more details like country availability.
        """
        afterpay_clearpay: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsAfterpayClearpay"
        ]
        """
        Afterpay gives your customers a way to pay for purchases in installments, check this [page](https://stripe.com/docs/payments/afterpay-clearpay) for more details like country availability. Afterpay is particularly popular among businesses selling fashion, beauty, and sports products.
        """
        alipay: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsAlipay"
        ]
        """
        Alipay is a digital wallet in China that has more than a billion active users worldwide. Alipay users can pay on the web or on a mobile device using login credentials or their Alipay app. Alipay has a low dispute rate and reduces fraud by authenticating payments using the customer's login credentials. Check this [page](https://stripe.com/docs/payments/alipay) for more details.
        """
        amazon_pay: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsAmazonPay"
        ]
        """
        Amazon Pay is a wallet payment method that lets your customers check out the same way as on Amazon.
        """
        apple_pay: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsApplePay"
        ]
        """
        Stripe users can accept [Apple Pay](https://stripe.com/payments/apple-pay) in iOS applications in iOS 9 and later, and on the web in Safari starting with iOS 10 or macOS Sierra. There are no additional fees to process Apple Pay payments, and the [pricing](https://stripe.com/pricing) is the same as other card transactions. Check this [page](https://stripe.com/docs/apple-pay) for more details.
        """
        apple_pay_later: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsApplePayLater"
        ]
        """
        Apple Pay Later, a payment method for customers to buy now and pay later, gives your customers a way to split purchases into four installments across six weeks.
        """
        au_becs_debit: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsAuBecsDebit"
        ]
        """
        Stripe users in Australia can accept Bulk Electronic Clearing System (BECS) direct debit payments from customers with an Australian bank account. Check this [page](https://stripe.com/docs/payments/au-becs-debit) for more details.
        """
        bacs_debit: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsBacsDebit"
        ]
        """
        Stripe users in the UK can accept Bacs Direct Debit payments from customers with a UK bank account, check this [page](https://stripe.com/docs/payments/payment-methods/bacs-debit) for more details.
        """
        bancontact: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsBancontact"
        ]
        """
        Bancontact is the most popular online payment method in Belgium, with over 15 million cards in circulation. [Customers](https://stripe.com/docs/api/customers) use a Bancontact card or mobile app linked to a Belgian bank account to make online payments that are secure, guaranteed, and confirmed immediately. Check this [page](https://stripe.com/docs/payments/bancontact) for more details.
        """
        blik: NotRequired["PaymentMethodConfigurationService.CreateParamsBlik"]
        """
        BLIK is a [single use](https://stripe.com/docs/payments/payment-methods#usage) payment method that requires customers to authenticate their payments. When customers want to pay online using BLIK, they request a six-digit code from their banking application and enter it into the payment collection form. Check this [page](https://stripe.com/docs/payments/blik) for more details.
        """
        boleto: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsBoleto"
        ]
        """
        Boleto is an official (regulated by the Central Bank of Brazil) payment method in Brazil. Check this [page](https://stripe.com/docs/payments/boleto) for more details.
        """
        card: NotRequired["PaymentMethodConfigurationService.CreateParamsCard"]
        """
        Cards are a popular way for consumers and businesses to pay online or in person. Stripe supports global and local card networks.
        """
        cartes_bancaires: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsCartesBancaires"
        ]
        """
        Cartes Bancaires is France's local card network. More than 95% of these cards are co-branded with either Visa or Mastercard, meaning you can process these cards over either Cartes Bancaires or the Visa or Mastercard networks. Check this [page](https://stripe.com/docs/payments/cartes-bancaires) for more details.
        """
        cashapp: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsCashapp"
        ]
        """
        Cash App is a popular consumer app in the US that allows customers to bank, invest, send, and receive money using their digital wallet. Check this [page](https://stripe.com/docs/payments/cash-app-pay) for more details.
        """
        customer_balance: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsCustomerBalance"
        ]
        """
        Uses a customer's [cash balance](https://stripe.com/docs/payments/customer-balance) for the payment. The cash balance can be funded via a bank transfer. Check this [page](https://stripe.com/docs/payments/bank-transfers) for more details.
        """
        eps: NotRequired["PaymentMethodConfigurationService.CreateParamsEps"]
        """
        EPS is an Austria-based payment method that allows customers to complete transactions online using their bank credentials. EPS is supported by all Austrian banks and is accepted by over 80% of Austrian online retailers. Check this [page](https://stripe.com/docs/payments/eps) for more details.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        fpx: NotRequired["PaymentMethodConfigurationService.CreateParamsFpx"]
        """
        Financial Process Exchange (FPX) is a Malaysia-based payment method that allows customers to complete transactions online using their bank credentials. Bank Negara Malaysia (BNM), the Central Bank of Malaysia, and eleven other major Malaysian financial institutions are members of the PayNet Group, which owns and operates FPX. It is one of the most popular online payment methods in Malaysia, with nearly 90 million transactions in 2018 according to BNM. Check this [page](https://stripe.com/docs/payments/fpx) for more details.
        """
        giropay: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsGiropay"
        ]
        """
        giropay is a German payment method based on online banking, introduced in 2006. It allows customers to complete transactions online using their online banking environment, with funds debited from their bank account. Depending on their bank, customers confirm payments on giropay using a second factor of authentication or a PIN. giropay accounts for 10% of online checkouts in Germany. Check this [page](https://stripe.com/docs/payments/giropay) for more details.
        """
        google_pay: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsGooglePay"
        ]
        """
        Google Pay allows customers to make payments in your app or website using any credit or debit card saved to their Google Account, including those from Google Play, YouTube, Chrome, or an Android device. Use the Google Pay API to request any credit or debit card stored in your customer's Google account. Check this [page](https://stripe.com/docs/google-pay) for more details.
        """
        grabpay: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsGrabpay"
        ]
        """
        GrabPay is a payment method developed by [Grab](https://www.grab.com/sg/consumer/finance/pay/). GrabPay is a digital wallet - customers maintain a balance in their wallets that they pay out with. Check this [page](https://stripe.com/docs/payments/grabpay) for more details.
        """
        ideal: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsIdeal"
        ]
        """
        iDEAL is a Netherlands-based payment method that allows customers to complete transactions online using their bank credentials. All major Dutch banks are members of Currence, the scheme that operates iDEAL, making it the most popular online payment method in the Netherlands with a share of online transactions close to 55%. Check this [page](https://stripe.com/docs/payments/ideal) for more details.
        """
        jcb: NotRequired["PaymentMethodConfigurationService.CreateParamsJcb"]
        """
        JCB is a credit card company based in Japan. JCB is currently available in Japan to businesses approved by JCB, and available to all businesses in Australia, Canada, Hong Kong, Japan, New Zealand, Singapore, Switzerland, United Kingdom, United States, and all countries in the European Economic Area except Iceland. Check this [page](https://support.stripe.com/questions/accepting-japan-credit-bureau-%28jcb%29-payments) for more details.
        """
        klarna: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsKlarna"
        ]
        """
        Klarna gives customers a range of [payment options](https://stripe.com/docs/payments/klarna#payment-options) during checkout. Available payment options vary depending on the customer's billing address and the transaction amount. These payment options make it convenient for customers to purchase items in all price ranges. Check this [page](https://stripe.com/docs/payments/klarna) for more details.
        """
        konbini: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsKonbini"
        ]
        """
        Konbini allows customers in Japan to pay for bills and online purchases at convenience stores with cash. Check this [page](https://stripe.com/docs/payments/konbini) for more details.
        """
        link: NotRequired["PaymentMethodConfigurationService.CreateParamsLink"]
        """
        [Link](https://stripe.com/docs/payments/link) is a payment method network. With Link, users save their payment details once, then reuse that information to pay with one click for any business on the network.
        """
        mobilepay: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsMobilepay"
        ]
        """
        MobilePay is a [single-use](https://stripe.com/docs/payments/payment-methods#usage) card wallet payment method used in Denmark and Finland. It allows customers to [authenticate and approve](https://stripe.com/docs/payments/payment-methods#customer-actions) payments using the MobilePay app. Check this [page](https://stripe.com/docs/payments/mobilepay) for more details.
        """
        multibanco: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsMultibanco"
        ]
        """
        Stripe users in Europe and the United States can accept Multibanco payments from customers in Portugal using [Sources](https://stripe.com/docs/sources)—a single integration path for creating payments using any supported method.
        """
        name: NotRequired[str]
        """
        Configuration name.
        """
        oxxo: NotRequired["PaymentMethodConfigurationService.CreateParamsOxxo"]
        """
        OXXO is a Mexican chain of convenience stores with thousands of locations across Latin America and represents nearly 20% of online transactions in Mexico. OXXO allows customers to pay bills and online purchases in-store with cash. Check this [page](https://stripe.com/docs/payments/oxxo) for more details.
        """
        p24: NotRequired["PaymentMethodConfigurationService.CreateParamsP24"]
        """
        Przelewy24 is a Poland-based payment method aggregator that allows customers to complete transactions online using bank transfers and other methods. Bank transfers account for 30% of online payments in Poland and Przelewy24 provides a way for customers to pay with over 165 banks. Check this [page](https://stripe.com/docs/payments/p24) for more details.
        """
        parent: NotRequired[str]
        """
        Configuration's parent configuration. Specify to create a child configuration.
        """
        paynow: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsPaynow"
        ]
        """
        PayNow is a Singapore-based payment method that allows customers to make a payment using their preferred app from participating banks and participating non-bank financial institutions. Check this [page](https://stripe.com/docs/payments/paynow) for more details.
        """
        paypal: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsPaypal"
        ]
        """
        PayPal, a digital wallet popular with customers in Europe, allows your customers worldwide to pay using their PayPal account. Check this [page](https://stripe.com/docs/payments/paypal) for more details.
        """
        promptpay: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsPromptpay"
        ]
        """
        PromptPay is a Thailand-based payment method that allows customers to make a payment using their preferred app from participating banks. Check this [page](https://stripe.com/docs/payments/promptpay) for more details.
        """
        revolut_pay: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsRevolutPay"
        ]
        """
        Revolut Pay, developed by Revolut, a global finance app, is a digital wallet payment method. Revolut Pay uses the customer's stored balance or cards to fund the payment, and offers the option for non-Revolut customers to save their details after their first purchase.
        """
        sepa_debit: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsSepaDebit"
        ]
        """
        The [Single Euro Payments Area (SEPA)](https://en.wikipedia.org/wiki/Single_Euro_Payments_Area) is an initiative of the European Union to simplify payments within and across member countries. SEPA established and enforced banking standards to allow for the direct debiting of every EUR-denominated bank account within the SEPA region, check this [page](https://stripe.com/docs/payments/sepa-debit) for more details.
        """
        sofort: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsSofort"
        ]
        """
        Stripe users in Europe and the United States can use the [Payment Intents API](https://stripe.com/docs/payments/payment-intents)—a single integration path for creating payments using any supported method—to accept [Sofort](https://www.sofort.com/) payments from customers. Check this [page](https://stripe.com/docs/payments/sofort) for more details.
        """
        swish: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsSwish"
        ]
        """
        Swish is a [real-time](https://stripe.com/docs/payments/real-time) payment method popular in Sweden. It allows customers to [authenticate and approve](https://stripe.com/docs/payments/payment-methods#customer-actions) payments using the Swish mobile app and the Swedish BankID mobile app. Check this [page](https://stripe.com/docs/payments/swish) for more details.
        """
        twint: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsTwint"
        ]
        """
        Twint is a payment method popular in Switzerland. It allows customers to pay using their mobile phone. Check this [page](https://docs.stripe.com/payments/twint) for more details.
        """
        us_bank_account: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsUsBankAccount"
        ]
        """
        Stripe users in the United States can accept ACH direct debit payments from customers with a US bank account using the Automated Clearing House (ACH) payments system operated by Nacha. Check this [page](https://stripe.com/docs/payments/ach-debit) for more details.
        """
        wechat_pay: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsWechatPay"
        ]
        """
        WeChat, owned by Tencent, is China's leading mobile app with over 1 billion monthly active users. Chinese consumers can use WeChat Pay to pay for goods and services inside of businesses' apps and websites. WeChat Pay users buy most frequently in gaming, e-commerce, travel, online education, and food/nutrition. Check this [page](https://stripe.com/docs/payments/wechat-pay) for more details.
        """
        zip: NotRequired["PaymentMethodConfigurationService.CreateParamsZip"]
        """
        Zip gives your customers a way to split purchases over a series of payments. Check this [page](https://stripe.com/docs/payments/zip) for more details like country availability.
        """

    class CreateParamsAcssDebit(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsAcssDebitDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class CreateParamsAcssDebitDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class CreateParamsAffirm(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsAffirmDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class CreateParamsAffirmDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class CreateParamsAfterpayClearpay(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsAfterpayClearpayDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class CreateParamsAfterpayClearpayDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class CreateParamsAlipay(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsAlipayDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class CreateParamsAlipayDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class CreateParamsAmazonPay(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsAmazonPayDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class CreateParamsAmazonPayDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class CreateParamsApplePay(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsApplePayDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class CreateParamsApplePayDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class CreateParamsApplePayLater(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsApplePayLaterDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class CreateParamsApplePayLaterDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class CreateParamsAuBecsDebit(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsAuBecsDebitDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class CreateParamsAuBecsDebitDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class CreateParamsBacsDebit(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsBacsDebitDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class CreateParamsBacsDebitDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class CreateParamsBancontact(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsBancontactDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class CreateParamsBancontactDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class CreateParamsBlik(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsBlikDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class CreateParamsBlikDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class CreateParamsBoleto(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsBoletoDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class CreateParamsBoletoDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class CreateParamsCard(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsCardDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class CreateParamsCardDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class CreateParamsCartesBancaires(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsCartesBancairesDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class CreateParamsCartesBancairesDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class CreateParamsCashapp(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsCashappDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class CreateParamsCashappDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class CreateParamsCustomerBalance(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsCustomerBalanceDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class CreateParamsCustomerBalanceDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class CreateParamsEps(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsEpsDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class CreateParamsEpsDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class CreateParamsFpx(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsFpxDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class CreateParamsFpxDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class CreateParamsGiropay(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsGiropayDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class CreateParamsGiropayDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class CreateParamsGooglePay(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsGooglePayDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class CreateParamsGooglePayDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class CreateParamsGrabpay(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsGrabpayDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class CreateParamsGrabpayDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class CreateParamsIdeal(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsIdealDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class CreateParamsIdealDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class CreateParamsJcb(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsJcbDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class CreateParamsJcbDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class CreateParamsKlarna(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsKlarnaDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class CreateParamsKlarnaDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class CreateParamsKonbini(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsKonbiniDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class CreateParamsKonbiniDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class CreateParamsLink(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsLinkDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class CreateParamsLinkDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class CreateParamsMobilepay(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsMobilepayDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class CreateParamsMobilepayDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class CreateParamsMultibanco(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsMultibancoDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class CreateParamsMultibancoDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class CreateParamsOxxo(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsOxxoDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class CreateParamsOxxoDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class CreateParamsP24(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsP24DisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class CreateParamsP24DisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class CreateParamsPaynow(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsPaynowDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class CreateParamsPaynowDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class CreateParamsPaypal(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsPaypalDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class CreateParamsPaypalDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class CreateParamsPromptpay(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsPromptpayDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class CreateParamsPromptpayDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class CreateParamsRevolutPay(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsRevolutPayDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class CreateParamsRevolutPayDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class CreateParamsSepaDebit(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsSepaDebitDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class CreateParamsSepaDebitDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class CreateParamsSofort(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsSofortDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class CreateParamsSofortDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class CreateParamsSwish(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsSwishDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class CreateParamsSwishDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class CreateParamsTwint(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsTwintDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class CreateParamsTwintDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class CreateParamsUsBankAccount(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsUsBankAccountDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class CreateParamsUsBankAccountDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class CreateParamsWechatPay(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsWechatPayDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class CreateParamsWechatPayDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class CreateParamsZip(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.CreateParamsZipDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class CreateParamsZipDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class ListParams(TypedDict):
        application: NotRequired["Literal['']|str"]
        """
        The Connect application to filter by.
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

    class RetrieveParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class UpdateParams(TypedDict):
        acss_debit: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsAcssDebit"
        ]
        """
        Canadian pre-authorized debit payments, check this [page](https://stripe.com/docs/payments/acss-debit) for more details like country availability.
        """
        active: NotRequired[bool]
        """
        Whether the configuration can be used for new payments.
        """
        affirm: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsAffirm"
        ]
        """
        [Affirm](https://www.affirm.com/) gives your customers a way to split purchases over a series of payments. Depending on the purchase, they can pay with four interest-free payments (Split Pay) or pay over a longer term (Installments), which might include interest. Check this [page](https://stripe.com/docs/payments/affirm) for more details like country availability.
        """
        afterpay_clearpay: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsAfterpayClearpay"
        ]
        """
        Afterpay gives your customers a way to pay for purchases in installments, check this [page](https://stripe.com/docs/payments/afterpay-clearpay) for more details like country availability. Afterpay is particularly popular among businesses selling fashion, beauty, and sports products.
        """
        alipay: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsAlipay"
        ]
        """
        Alipay is a digital wallet in China that has more than a billion active users worldwide. Alipay users can pay on the web or on a mobile device using login credentials or their Alipay app. Alipay has a low dispute rate and reduces fraud by authenticating payments using the customer's login credentials. Check this [page](https://stripe.com/docs/payments/alipay) for more details.
        """
        amazon_pay: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsAmazonPay"
        ]
        """
        Amazon Pay is a wallet payment method that lets your customers check out the same way as on Amazon.
        """
        apple_pay: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsApplePay"
        ]
        """
        Stripe users can accept [Apple Pay](https://stripe.com/payments/apple-pay) in iOS applications in iOS 9 and later, and on the web in Safari starting with iOS 10 or macOS Sierra. There are no additional fees to process Apple Pay payments, and the [pricing](https://stripe.com/pricing) is the same as other card transactions. Check this [page](https://stripe.com/docs/apple-pay) for more details.
        """
        apple_pay_later: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsApplePayLater"
        ]
        """
        Apple Pay Later, a payment method for customers to buy now and pay later, gives your customers a way to split purchases into four installments across six weeks.
        """
        au_becs_debit: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsAuBecsDebit"
        ]
        """
        Stripe users in Australia can accept Bulk Electronic Clearing System (BECS) direct debit payments from customers with an Australian bank account. Check this [page](https://stripe.com/docs/payments/au-becs-debit) for more details.
        """
        bacs_debit: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsBacsDebit"
        ]
        """
        Stripe users in the UK can accept Bacs Direct Debit payments from customers with a UK bank account, check this [page](https://stripe.com/docs/payments/payment-methods/bacs-debit) for more details.
        """
        bancontact: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsBancontact"
        ]
        """
        Bancontact is the most popular online payment method in Belgium, with over 15 million cards in circulation. [Customers](https://stripe.com/docs/api/customers) use a Bancontact card or mobile app linked to a Belgian bank account to make online payments that are secure, guaranteed, and confirmed immediately. Check this [page](https://stripe.com/docs/payments/bancontact) for more details.
        """
        blik: NotRequired["PaymentMethodConfigurationService.UpdateParamsBlik"]
        """
        BLIK is a [single use](https://stripe.com/docs/payments/payment-methods#usage) payment method that requires customers to authenticate their payments. When customers want to pay online using BLIK, they request a six-digit code from their banking application and enter it into the payment collection form. Check this [page](https://stripe.com/docs/payments/blik) for more details.
        """
        boleto: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsBoleto"
        ]
        """
        Boleto is an official (regulated by the Central Bank of Brazil) payment method in Brazil. Check this [page](https://stripe.com/docs/payments/boleto) for more details.
        """
        card: NotRequired["PaymentMethodConfigurationService.UpdateParamsCard"]
        """
        Cards are a popular way for consumers and businesses to pay online or in person. Stripe supports global and local card networks.
        """
        cartes_bancaires: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsCartesBancaires"
        ]
        """
        Cartes Bancaires is France's local card network. More than 95% of these cards are co-branded with either Visa or Mastercard, meaning you can process these cards over either Cartes Bancaires or the Visa or Mastercard networks. Check this [page](https://stripe.com/docs/payments/cartes-bancaires) for more details.
        """
        cashapp: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsCashapp"
        ]
        """
        Cash App is a popular consumer app in the US that allows customers to bank, invest, send, and receive money using their digital wallet. Check this [page](https://stripe.com/docs/payments/cash-app-pay) for more details.
        """
        customer_balance: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsCustomerBalance"
        ]
        """
        Uses a customer's [cash balance](https://stripe.com/docs/payments/customer-balance) for the payment. The cash balance can be funded via a bank transfer. Check this [page](https://stripe.com/docs/payments/bank-transfers) for more details.
        """
        eps: NotRequired["PaymentMethodConfigurationService.UpdateParamsEps"]
        """
        EPS is an Austria-based payment method that allows customers to complete transactions online using their bank credentials. EPS is supported by all Austrian banks and is accepted by over 80% of Austrian online retailers. Check this [page](https://stripe.com/docs/payments/eps) for more details.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        fpx: NotRequired["PaymentMethodConfigurationService.UpdateParamsFpx"]
        """
        Financial Process Exchange (FPX) is a Malaysia-based payment method that allows customers to complete transactions online using their bank credentials. Bank Negara Malaysia (BNM), the Central Bank of Malaysia, and eleven other major Malaysian financial institutions are members of the PayNet Group, which owns and operates FPX. It is one of the most popular online payment methods in Malaysia, with nearly 90 million transactions in 2018 according to BNM. Check this [page](https://stripe.com/docs/payments/fpx) for more details.
        """
        giropay: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsGiropay"
        ]
        """
        giropay is a German payment method based on online banking, introduced in 2006. It allows customers to complete transactions online using their online banking environment, with funds debited from their bank account. Depending on their bank, customers confirm payments on giropay using a second factor of authentication or a PIN. giropay accounts for 10% of online checkouts in Germany. Check this [page](https://stripe.com/docs/payments/giropay) for more details.
        """
        google_pay: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsGooglePay"
        ]
        """
        Google Pay allows customers to make payments in your app or website using any credit or debit card saved to their Google Account, including those from Google Play, YouTube, Chrome, or an Android device. Use the Google Pay API to request any credit or debit card stored in your customer's Google account. Check this [page](https://stripe.com/docs/google-pay) for more details.
        """
        grabpay: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsGrabpay"
        ]
        """
        GrabPay is a payment method developed by [Grab](https://www.grab.com/sg/consumer/finance/pay/). GrabPay is a digital wallet - customers maintain a balance in their wallets that they pay out with. Check this [page](https://stripe.com/docs/payments/grabpay) for more details.
        """
        ideal: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsIdeal"
        ]
        """
        iDEAL is a Netherlands-based payment method that allows customers to complete transactions online using their bank credentials. All major Dutch banks are members of Currence, the scheme that operates iDEAL, making it the most popular online payment method in the Netherlands with a share of online transactions close to 55%. Check this [page](https://stripe.com/docs/payments/ideal) for more details.
        """
        jcb: NotRequired["PaymentMethodConfigurationService.UpdateParamsJcb"]
        """
        JCB is a credit card company based in Japan. JCB is currently available in Japan to businesses approved by JCB, and available to all businesses in Australia, Canada, Hong Kong, Japan, New Zealand, Singapore, Switzerland, United Kingdom, United States, and all countries in the European Economic Area except Iceland. Check this [page](https://support.stripe.com/questions/accepting-japan-credit-bureau-%28jcb%29-payments) for more details.
        """
        klarna: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsKlarna"
        ]
        """
        Klarna gives customers a range of [payment options](https://stripe.com/docs/payments/klarna#payment-options) during checkout. Available payment options vary depending on the customer's billing address and the transaction amount. These payment options make it convenient for customers to purchase items in all price ranges. Check this [page](https://stripe.com/docs/payments/klarna) for more details.
        """
        konbini: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsKonbini"
        ]
        """
        Konbini allows customers in Japan to pay for bills and online purchases at convenience stores with cash. Check this [page](https://stripe.com/docs/payments/konbini) for more details.
        """
        link: NotRequired["PaymentMethodConfigurationService.UpdateParamsLink"]
        """
        [Link](https://stripe.com/docs/payments/link) is a payment method network. With Link, users save their payment details once, then reuse that information to pay with one click for any business on the network.
        """
        mobilepay: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsMobilepay"
        ]
        """
        MobilePay is a [single-use](https://stripe.com/docs/payments/payment-methods#usage) card wallet payment method used in Denmark and Finland. It allows customers to [authenticate and approve](https://stripe.com/docs/payments/payment-methods#customer-actions) payments using the MobilePay app. Check this [page](https://stripe.com/docs/payments/mobilepay) for more details.
        """
        multibanco: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsMultibanco"
        ]
        """
        Stripe users in Europe and the United States can accept Multibanco payments from customers in Portugal using [Sources](https://stripe.com/docs/sources)—a single integration path for creating payments using any supported method.
        """
        name: NotRequired[str]
        """
        Configuration name.
        """
        oxxo: NotRequired["PaymentMethodConfigurationService.UpdateParamsOxxo"]
        """
        OXXO is a Mexican chain of convenience stores with thousands of locations across Latin America and represents nearly 20% of online transactions in Mexico. OXXO allows customers to pay bills and online purchases in-store with cash. Check this [page](https://stripe.com/docs/payments/oxxo) for more details.
        """
        p24: NotRequired["PaymentMethodConfigurationService.UpdateParamsP24"]
        """
        Przelewy24 is a Poland-based payment method aggregator that allows customers to complete transactions online using bank transfers and other methods. Bank transfers account for 30% of online payments in Poland and Przelewy24 provides a way for customers to pay with over 165 banks. Check this [page](https://stripe.com/docs/payments/p24) for more details.
        """
        paynow: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsPaynow"
        ]
        """
        PayNow is a Singapore-based payment method that allows customers to make a payment using their preferred app from participating banks and participating non-bank financial institutions. Check this [page](https://stripe.com/docs/payments/paynow) for more details.
        """
        paypal: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsPaypal"
        ]
        """
        PayPal, a digital wallet popular with customers in Europe, allows your customers worldwide to pay using their PayPal account. Check this [page](https://stripe.com/docs/payments/paypal) for more details.
        """
        promptpay: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsPromptpay"
        ]
        """
        PromptPay is a Thailand-based payment method that allows customers to make a payment using their preferred app from participating banks. Check this [page](https://stripe.com/docs/payments/promptpay) for more details.
        """
        revolut_pay: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsRevolutPay"
        ]
        """
        Revolut Pay, developed by Revolut, a global finance app, is a digital wallet payment method. Revolut Pay uses the customer's stored balance or cards to fund the payment, and offers the option for non-Revolut customers to save their details after their first purchase.
        """
        sepa_debit: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsSepaDebit"
        ]
        """
        The [Single Euro Payments Area (SEPA)](https://en.wikipedia.org/wiki/Single_Euro_Payments_Area) is an initiative of the European Union to simplify payments within and across member countries. SEPA established and enforced banking standards to allow for the direct debiting of every EUR-denominated bank account within the SEPA region, check this [page](https://stripe.com/docs/payments/sepa-debit) for more details.
        """
        sofort: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsSofort"
        ]
        """
        Stripe users in Europe and the United States can use the [Payment Intents API](https://stripe.com/docs/payments/payment-intents)—a single integration path for creating payments using any supported method—to accept [Sofort](https://www.sofort.com/) payments from customers. Check this [page](https://stripe.com/docs/payments/sofort) for more details.
        """
        swish: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsSwish"
        ]
        """
        Swish is a [real-time](https://stripe.com/docs/payments/real-time) payment method popular in Sweden. It allows customers to [authenticate and approve](https://stripe.com/docs/payments/payment-methods#customer-actions) payments using the Swish mobile app and the Swedish BankID mobile app. Check this [page](https://stripe.com/docs/payments/swish) for more details.
        """
        twint: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsTwint"
        ]
        """
        Twint is a payment method popular in Switzerland. It allows customers to pay using their mobile phone. Check this [page](https://docs.stripe.com/payments/twint) for more details.
        """
        us_bank_account: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsUsBankAccount"
        ]
        """
        Stripe users in the United States can accept ACH direct debit payments from customers with a US bank account using the Automated Clearing House (ACH) payments system operated by Nacha. Check this [page](https://stripe.com/docs/payments/ach-debit) for more details.
        """
        wechat_pay: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsWechatPay"
        ]
        """
        WeChat, owned by Tencent, is China's leading mobile app with over 1 billion monthly active users. Chinese consumers can use WeChat Pay to pay for goods and services inside of businesses' apps and websites. WeChat Pay users buy most frequently in gaming, e-commerce, travel, online education, and food/nutrition. Check this [page](https://stripe.com/docs/payments/wechat-pay) for more details.
        """
        zip: NotRequired["PaymentMethodConfigurationService.UpdateParamsZip"]
        """
        Zip gives your customers a way to split purchases over a series of payments. Check this [page](https://stripe.com/docs/payments/zip) for more details like country availability.
        """

    class UpdateParamsAcssDebit(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsAcssDebitDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class UpdateParamsAcssDebitDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class UpdateParamsAffirm(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsAffirmDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class UpdateParamsAffirmDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class UpdateParamsAfterpayClearpay(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsAfterpayClearpayDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class UpdateParamsAfterpayClearpayDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class UpdateParamsAlipay(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsAlipayDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class UpdateParamsAlipayDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class UpdateParamsAmazonPay(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsAmazonPayDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class UpdateParamsAmazonPayDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class UpdateParamsApplePay(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsApplePayDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class UpdateParamsApplePayDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class UpdateParamsApplePayLater(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsApplePayLaterDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class UpdateParamsApplePayLaterDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class UpdateParamsAuBecsDebit(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsAuBecsDebitDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class UpdateParamsAuBecsDebitDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class UpdateParamsBacsDebit(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsBacsDebitDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class UpdateParamsBacsDebitDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class UpdateParamsBancontact(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsBancontactDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class UpdateParamsBancontactDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class UpdateParamsBlik(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsBlikDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class UpdateParamsBlikDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class UpdateParamsBoleto(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsBoletoDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class UpdateParamsBoletoDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class UpdateParamsCard(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsCardDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class UpdateParamsCardDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class UpdateParamsCartesBancaires(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsCartesBancairesDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class UpdateParamsCartesBancairesDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class UpdateParamsCashapp(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsCashappDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class UpdateParamsCashappDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class UpdateParamsCustomerBalance(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsCustomerBalanceDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class UpdateParamsCustomerBalanceDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class UpdateParamsEps(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsEpsDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class UpdateParamsEpsDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class UpdateParamsFpx(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsFpxDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class UpdateParamsFpxDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class UpdateParamsGiropay(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsGiropayDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class UpdateParamsGiropayDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class UpdateParamsGooglePay(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsGooglePayDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class UpdateParamsGooglePayDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class UpdateParamsGrabpay(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsGrabpayDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class UpdateParamsGrabpayDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class UpdateParamsIdeal(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsIdealDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class UpdateParamsIdealDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class UpdateParamsJcb(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsJcbDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class UpdateParamsJcbDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class UpdateParamsKlarna(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsKlarnaDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class UpdateParamsKlarnaDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class UpdateParamsKonbini(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsKonbiniDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class UpdateParamsKonbiniDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class UpdateParamsLink(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsLinkDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class UpdateParamsLinkDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class UpdateParamsMobilepay(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsMobilepayDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class UpdateParamsMobilepayDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class UpdateParamsMultibanco(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsMultibancoDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class UpdateParamsMultibancoDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class UpdateParamsOxxo(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsOxxoDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class UpdateParamsOxxoDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class UpdateParamsP24(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsP24DisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class UpdateParamsP24DisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class UpdateParamsPaynow(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsPaynowDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class UpdateParamsPaynowDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class UpdateParamsPaypal(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsPaypalDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class UpdateParamsPaypalDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class UpdateParamsPromptpay(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsPromptpayDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class UpdateParamsPromptpayDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class UpdateParamsRevolutPay(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsRevolutPayDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class UpdateParamsRevolutPayDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class UpdateParamsSepaDebit(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsSepaDebitDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class UpdateParamsSepaDebitDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class UpdateParamsSofort(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsSofortDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class UpdateParamsSofortDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class UpdateParamsSwish(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsSwishDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class UpdateParamsSwishDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class UpdateParamsTwint(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsTwintDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class UpdateParamsTwintDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class UpdateParamsUsBankAccount(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsUsBankAccountDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class UpdateParamsUsBankAccountDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class UpdateParamsWechatPay(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsWechatPayDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class UpdateParamsWechatPayDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class UpdateParamsZip(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfigurationService.UpdateParamsZipDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class UpdateParamsZipDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    def list(
        self,
        params: "PaymentMethodConfigurationService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[PaymentMethodConfiguration]:
        """
        List payment method configurations
        """
        return cast(
            ListObject[PaymentMethodConfiguration],
            self._request(
                "get",
                "/v1/payment_method_configurations",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "PaymentMethodConfigurationService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[PaymentMethodConfiguration]:
        """
        List payment method configurations
        """
        return cast(
            ListObject[PaymentMethodConfiguration],
            await self._request_async(
                "get",
                "/v1/payment_method_configurations",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self,
        params: "PaymentMethodConfigurationService.CreateParams" = {},
        options: RequestOptions = {},
    ) -> PaymentMethodConfiguration:
        """
        Creates a payment method configuration
        """
        return cast(
            PaymentMethodConfiguration,
            self._request(
                "post",
                "/v1/payment_method_configurations",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        params: "PaymentMethodConfigurationService.CreateParams" = {},
        options: RequestOptions = {},
    ) -> PaymentMethodConfiguration:
        """
        Creates a payment method configuration
        """
        return cast(
            PaymentMethodConfiguration,
            await self._request_async(
                "post",
                "/v1/payment_method_configurations",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        configuration: str,
        params: "PaymentMethodConfigurationService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> PaymentMethodConfiguration:
        """
        Retrieve payment method configuration
        """
        return cast(
            PaymentMethodConfiguration,
            self._request(
                "get",
                "/v1/payment_method_configurations/{configuration}".format(
                    configuration=sanitize_id(configuration),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        configuration: str,
        params: "PaymentMethodConfigurationService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> PaymentMethodConfiguration:
        """
        Retrieve payment method configuration
        """
        return cast(
            PaymentMethodConfiguration,
            await self._request_async(
                "get",
                "/v1/payment_method_configurations/{configuration}".format(
                    configuration=sanitize_id(configuration),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def update(
        self,
        configuration: str,
        params: "PaymentMethodConfigurationService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> PaymentMethodConfiguration:
        """
        Update payment method configuration
        """
        return cast(
            PaymentMethodConfiguration,
            self._request(
                "post",
                "/v1/payment_method_configurations/{configuration}".format(
                    configuration=sanitize_id(configuration),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def update_async(
        self,
        configuration: str,
        params: "PaymentMethodConfigurationService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> PaymentMethodConfiguration:
        """
        Update payment method configuration
        """
        return cast(
            PaymentMethodConfiguration,
            await self._request_async(
                "post",
                "/v1/payment_method_configurations/{configuration}".format(
                    configuration=sanitize_id(configuration),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

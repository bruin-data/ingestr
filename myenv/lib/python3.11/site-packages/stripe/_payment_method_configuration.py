# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._createable_api_resource import CreateableAPIResource
from stripe._list_object import ListObject
from stripe._listable_api_resource import ListableAPIResource
from stripe._request_options import RequestOptions
from stripe._stripe_object import StripeObject
from stripe._updateable_api_resource import UpdateableAPIResource
from stripe._util import sanitize_id
from typing import ClassVar, List, Optional, cast
from typing_extensions import Literal, NotRequired, TypedDict, Unpack


class PaymentMethodConfiguration(
    CreateableAPIResource["PaymentMethodConfiguration"],
    ListableAPIResource["PaymentMethodConfiguration"],
    UpdateableAPIResource["PaymentMethodConfiguration"],
):
    """
    PaymentMethodConfigurations control which payment methods are displayed to your customers when you don't explicitly specify payment method types. You can have multiple configurations with different sets of payment methods for different scenarios.

    There are two types of PaymentMethodConfigurations. Which is used depends on the [charge type](https://stripe.com/docs/connect/charges):

    **Direct** configurations apply to payments created on your account, including Connect destination charges, Connect separate charges and transfers, and payments not involving Connect.

    **Child** configurations apply to payments created on your connected accounts using direct charges, and charges with the on_behalf_of parameter.

    Child configurations have a `parent` that sets default values and controls which settings connected accounts may override. You can specify a parent ID at payment time, and Stripe will automatically resolve the connected account's associated child configuration. Parent configurations are [managed in the dashboard](https://dashboard.stripe.com/settings/payment_methods/connected_accounts) and are not available in this API.

    Related guides:
    - [Payment Method Configurations API](https://stripe.com/docs/connect/payment-method-configurations)
    - [Multiple configurations on dynamic payment methods](https://stripe.com/docs/payments/multiple-payment-method-configs)
    - [Multiple configurations for your Connect accounts](https://stripe.com/docs/connect/multiple-payment-method-configurations)
    """

    OBJECT_NAME: ClassVar[Literal["payment_method_configuration"]] = (
        "payment_method_configuration"
    )

    class AcssDebit(StripeObject):
        class DisplayPreference(StripeObject):
            overridable: Optional[bool]
            """
            For child configs, whether or not the account's preference will be observed. If `false`, the parent configuration's default is used.
            """
            preference: Literal["none", "off", "on"]
            """
            The account's display preference.
            """
            value: Literal["off", "on"]
            """
            The effective display preference value.
            """

        available: bool
        """
        Whether this payment method may be offered at checkout. True if `display_preference` is `on` and the payment method's capability is active.
        """
        display_preference: DisplayPreference
        _inner_class_types = {"display_preference": DisplayPreference}

    class Affirm(StripeObject):
        class DisplayPreference(StripeObject):
            overridable: Optional[bool]
            """
            For child configs, whether or not the account's preference will be observed. If `false`, the parent configuration's default is used.
            """
            preference: Literal["none", "off", "on"]
            """
            The account's display preference.
            """
            value: Literal["off", "on"]
            """
            The effective display preference value.
            """

        available: bool
        """
        Whether this payment method may be offered at checkout. True if `display_preference` is `on` and the payment method's capability is active.
        """
        display_preference: DisplayPreference
        _inner_class_types = {"display_preference": DisplayPreference}

    class AfterpayClearpay(StripeObject):
        class DisplayPreference(StripeObject):
            overridable: Optional[bool]
            """
            For child configs, whether or not the account's preference will be observed. If `false`, the parent configuration's default is used.
            """
            preference: Literal["none", "off", "on"]
            """
            The account's display preference.
            """
            value: Literal["off", "on"]
            """
            The effective display preference value.
            """

        available: bool
        """
        Whether this payment method may be offered at checkout. True if `display_preference` is `on` and the payment method's capability is active.
        """
        display_preference: DisplayPreference
        _inner_class_types = {"display_preference": DisplayPreference}

    class Alipay(StripeObject):
        class DisplayPreference(StripeObject):
            overridable: Optional[bool]
            """
            For child configs, whether or not the account's preference will be observed. If `false`, the parent configuration's default is used.
            """
            preference: Literal["none", "off", "on"]
            """
            The account's display preference.
            """
            value: Literal["off", "on"]
            """
            The effective display preference value.
            """

        available: bool
        """
        Whether this payment method may be offered at checkout. True if `display_preference` is `on` and the payment method's capability is active.
        """
        display_preference: DisplayPreference
        _inner_class_types = {"display_preference": DisplayPreference}

    class AmazonPay(StripeObject):
        class DisplayPreference(StripeObject):
            overridable: Optional[bool]
            """
            For child configs, whether or not the account's preference will be observed. If `false`, the parent configuration's default is used.
            """
            preference: Literal["none", "off", "on"]
            """
            The account's display preference.
            """
            value: Literal["off", "on"]
            """
            The effective display preference value.
            """

        available: bool
        """
        Whether this payment method may be offered at checkout. True if `display_preference` is `on` and the payment method's capability is active.
        """
        display_preference: DisplayPreference
        _inner_class_types = {"display_preference": DisplayPreference}

    class ApplePay(StripeObject):
        class DisplayPreference(StripeObject):
            overridable: Optional[bool]
            """
            For child configs, whether or not the account's preference will be observed. If `false`, the parent configuration's default is used.
            """
            preference: Literal["none", "off", "on"]
            """
            The account's display preference.
            """
            value: Literal["off", "on"]
            """
            The effective display preference value.
            """

        available: bool
        """
        Whether this payment method may be offered at checkout. True if `display_preference` is `on` and the payment method's capability is active.
        """
        display_preference: DisplayPreference
        _inner_class_types = {"display_preference": DisplayPreference}

    class AuBecsDebit(StripeObject):
        class DisplayPreference(StripeObject):
            overridable: Optional[bool]
            """
            For child configs, whether or not the account's preference will be observed. If `false`, the parent configuration's default is used.
            """
            preference: Literal["none", "off", "on"]
            """
            The account's display preference.
            """
            value: Literal["off", "on"]
            """
            The effective display preference value.
            """

        available: bool
        """
        Whether this payment method may be offered at checkout. True if `display_preference` is `on` and the payment method's capability is active.
        """
        display_preference: DisplayPreference
        _inner_class_types = {"display_preference": DisplayPreference}

    class BacsDebit(StripeObject):
        class DisplayPreference(StripeObject):
            overridable: Optional[bool]
            """
            For child configs, whether or not the account's preference will be observed. If `false`, the parent configuration's default is used.
            """
            preference: Literal["none", "off", "on"]
            """
            The account's display preference.
            """
            value: Literal["off", "on"]
            """
            The effective display preference value.
            """

        available: bool
        """
        Whether this payment method may be offered at checkout. True if `display_preference` is `on` and the payment method's capability is active.
        """
        display_preference: DisplayPreference
        _inner_class_types = {"display_preference": DisplayPreference}

    class Bancontact(StripeObject):
        class DisplayPreference(StripeObject):
            overridable: Optional[bool]
            """
            For child configs, whether or not the account's preference will be observed. If `false`, the parent configuration's default is used.
            """
            preference: Literal["none", "off", "on"]
            """
            The account's display preference.
            """
            value: Literal["off", "on"]
            """
            The effective display preference value.
            """

        available: bool
        """
        Whether this payment method may be offered at checkout. True if `display_preference` is `on` and the payment method's capability is active.
        """
        display_preference: DisplayPreference
        _inner_class_types = {"display_preference": DisplayPreference}

    class Blik(StripeObject):
        class DisplayPreference(StripeObject):
            overridable: Optional[bool]
            """
            For child configs, whether or not the account's preference will be observed. If `false`, the parent configuration's default is used.
            """
            preference: Literal["none", "off", "on"]
            """
            The account's display preference.
            """
            value: Literal["off", "on"]
            """
            The effective display preference value.
            """

        available: bool
        """
        Whether this payment method may be offered at checkout. True if `display_preference` is `on` and the payment method's capability is active.
        """
        display_preference: DisplayPreference
        _inner_class_types = {"display_preference": DisplayPreference}

    class Boleto(StripeObject):
        class DisplayPreference(StripeObject):
            overridable: Optional[bool]
            """
            For child configs, whether or not the account's preference will be observed. If `false`, the parent configuration's default is used.
            """
            preference: Literal["none", "off", "on"]
            """
            The account's display preference.
            """
            value: Literal["off", "on"]
            """
            The effective display preference value.
            """

        available: bool
        """
        Whether this payment method may be offered at checkout. True if `display_preference` is `on` and the payment method's capability is active.
        """
        display_preference: DisplayPreference
        _inner_class_types = {"display_preference": DisplayPreference}

    class Card(StripeObject):
        class DisplayPreference(StripeObject):
            overridable: Optional[bool]
            """
            For child configs, whether or not the account's preference will be observed. If `false`, the parent configuration's default is used.
            """
            preference: Literal["none", "off", "on"]
            """
            The account's display preference.
            """
            value: Literal["off", "on"]
            """
            The effective display preference value.
            """

        available: bool
        """
        Whether this payment method may be offered at checkout. True if `display_preference` is `on` and the payment method's capability is active.
        """
        display_preference: DisplayPreference
        _inner_class_types = {"display_preference": DisplayPreference}

    class CartesBancaires(StripeObject):
        class DisplayPreference(StripeObject):
            overridable: Optional[bool]
            """
            For child configs, whether or not the account's preference will be observed. If `false`, the parent configuration's default is used.
            """
            preference: Literal["none", "off", "on"]
            """
            The account's display preference.
            """
            value: Literal["off", "on"]
            """
            The effective display preference value.
            """

        available: bool
        """
        Whether this payment method may be offered at checkout. True if `display_preference` is `on` and the payment method's capability is active.
        """
        display_preference: DisplayPreference
        _inner_class_types = {"display_preference": DisplayPreference}

    class Cashapp(StripeObject):
        class DisplayPreference(StripeObject):
            overridable: Optional[bool]
            """
            For child configs, whether or not the account's preference will be observed. If `false`, the parent configuration's default is used.
            """
            preference: Literal["none", "off", "on"]
            """
            The account's display preference.
            """
            value: Literal["off", "on"]
            """
            The effective display preference value.
            """

        available: bool
        """
        Whether this payment method may be offered at checkout. True if `display_preference` is `on` and the payment method's capability is active.
        """
        display_preference: DisplayPreference
        _inner_class_types = {"display_preference": DisplayPreference}

    class CustomerBalance(StripeObject):
        class DisplayPreference(StripeObject):
            overridable: Optional[bool]
            """
            For child configs, whether or not the account's preference will be observed. If `false`, the parent configuration's default is used.
            """
            preference: Literal["none", "off", "on"]
            """
            The account's display preference.
            """
            value: Literal["off", "on"]
            """
            The effective display preference value.
            """

        available: bool
        """
        Whether this payment method may be offered at checkout. True if `display_preference` is `on` and the payment method's capability is active.
        """
        display_preference: DisplayPreference
        _inner_class_types = {"display_preference": DisplayPreference}

    class Eps(StripeObject):
        class DisplayPreference(StripeObject):
            overridable: Optional[bool]
            """
            For child configs, whether or not the account's preference will be observed. If `false`, the parent configuration's default is used.
            """
            preference: Literal["none", "off", "on"]
            """
            The account's display preference.
            """
            value: Literal["off", "on"]
            """
            The effective display preference value.
            """

        available: bool
        """
        Whether this payment method may be offered at checkout. True if `display_preference` is `on` and the payment method's capability is active.
        """
        display_preference: DisplayPreference
        _inner_class_types = {"display_preference": DisplayPreference}

    class Fpx(StripeObject):
        class DisplayPreference(StripeObject):
            overridable: Optional[bool]
            """
            For child configs, whether or not the account's preference will be observed. If `false`, the parent configuration's default is used.
            """
            preference: Literal["none", "off", "on"]
            """
            The account's display preference.
            """
            value: Literal["off", "on"]
            """
            The effective display preference value.
            """

        available: bool
        """
        Whether this payment method may be offered at checkout. True if `display_preference` is `on` and the payment method's capability is active.
        """
        display_preference: DisplayPreference
        _inner_class_types = {"display_preference": DisplayPreference}

    class Giropay(StripeObject):
        class DisplayPreference(StripeObject):
            overridable: Optional[bool]
            """
            For child configs, whether or not the account's preference will be observed. If `false`, the parent configuration's default is used.
            """
            preference: Literal["none", "off", "on"]
            """
            The account's display preference.
            """
            value: Literal["off", "on"]
            """
            The effective display preference value.
            """

        available: bool
        """
        Whether this payment method may be offered at checkout. True if `display_preference` is `on` and the payment method's capability is active.
        """
        display_preference: DisplayPreference
        _inner_class_types = {"display_preference": DisplayPreference}

    class GooglePay(StripeObject):
        class DisplayPreference(StripeObject):
            overridable: Optional[bool]
            """
            For child configs, whether or not the account's preference will be observed. If `false`, the parent configuration's default is used.
            """
            preference: Literal["none", "off", "on"]
            """
            The account's display preference.
            """
            value: Literal["off", "on"]
            """
            The effective display preference value.
            """

        available: bool
        """
        Whether this payment method may be offered at checkout. True if `display_preference` is `on` and the payment method's capability is active.
        """
        display_preference: DisplayPreference
        _inner_class_types = {"display_preference": DisplayPreference}

    class Grabpay(StripeObject):
        class DisplayPreference(StripeObject):
            overridable: Optional[bool]
            """
            For child configs, whether or not the account's preference will be observed. If `false`, the parent configuration's default is used.
            """
            preference: Literal["none", "off", "on"]
            """
            The account's display preference.
            """
            value: Literal["off", "on"]
            """
            The effective display preference value.
            """

        available: bool
        """
        Whether this payment method may be offered at checkout. True if `display_preference` is `on` and the payment method's capability is active.
        """
        display_preference: DisplayPreference
        _inner_class_types = {"display_preference": DisplayPreference}

    class Ideal(StripeObject):
        class DisplayPreference(StripeObject):
            overridable: Optional[bool]
            """
            For child configs, whether or not the account's preference will be observed. If `false`, the parent configuration's default is used.
            """
            preference: Literal["none", "off", "on"]
            """
            The account's display preference.
            """
            value: Literal["off", "on"]
            """
            The effective display preference value.
            """

        available: bool
        """
        Whether this payment method may be offered at checkout. True if `display_preference` is `on` and the payment method's capability is active.
        """
        display_preference: DisplayPreference
        _inner_class_types = {"display_preference": DisplayPreference}

    class Jcb(StripeObject):
        class DisplayPreference(StripeObject):
            overridable: Optional[bool]
            """
            For child configs, whether or not the account's preference will be observed. If `false`, the parent configuration's default is used.
            """
            preference: Literal["none", "off", "on"]
            """
            The account's display preference.
            """
            value: Literal["off", "on"]
            """
            The effective display preference value.
            """

        available: bool
        """
        Whether this payment method may be offered at checkout. True if `display_preference` is `on` and the payment method's capability is active.
        """
        display_preference: DisplayPreference
        _inner_class_types = {"display_preference": DisplayPreference}

    class Klarna(StripeObject):
        class DisplayPreference(StripeObject):
            overridable: Optional[bool]
            """
            For child configs, whether or not the account's preference will be observed. If `false`, the parent configuration's default is used.
            """
            preference: Literal["none", "off", "on"]
            """
            The account's display preference.
            """
            value: Literal["off", "on"]
            """
            The effective display preference value.
            """

        available: bool
        """
        Whether this payment method may be offered at checkout. True if `display_preference` is `on` and the payment method's capability is active.
        """
        display_preference: DisplayPreference
        _inner_class_types = {"display_preference": DisplayPreference}

    class Konbini(StripeObject):
        class DisplayPreference(StripeObject):
            overridable: Optional[bool]
            """
            For child configs, whether or not the account's preference will be observed. If `false`, the parent configuration's default is used.
            """
            preference: Literal["none", "off", "on"]
            """
            The account's display preference.
            """
            value: Literal["off", "on"]
            """
            The effective display preference value.
            """

        available: bool
        """
        Whether this payment method may be offered at checkout. True if `display_preference` is `on` and the payment method's capability is active.
        """
        display_preference: DisplayPreference
        _inner_class_types = {"display_preference": DisplayPreference}

    class Link(StripeObject):
        class DisplayPreference(StripeObject):
            overridable: Optional[bool]
            """
            For child configs, whether or not the account's preference will be observed. If `false`, the parent configuration's default is used.
            """
            preference: Literal["none", "off", "on"]
            """
            The account's display preference.
            """
            value: Literal["off", "on"]
            """
            The effective display preference value.
            """

        available: bool
        """
        Whether this payment method may be offered at checkout. True if `display_preference` is `on` and the payment method's capability is active.
        """
        display_preference: DisplayPreference
        _inner_class_types = {"display_preference": DisplayPreference}

    class Mobilepay(StripeObject):
        class DisplayPreference(StripeObject):
            overridable: Optional[bool]
            """
            For child configs, whether or not the account's preference will be observed. If `false`, the parent configuration's default is used.
            """
            preference: Literal["none", "off", "on"]
            """
            The account's display preference.
            """
            value: Literal["off", "on"]
            """
            The effective display preference value.
            """

        available: bool
        """
        Whether this payment method may be offered at checkout. True if `display_preference` is `on` and the payment method's capability is active.
        """
        display_preference: DisplayPreference
        _inner_class_types = {"display_preference": DisplayPreference}

    class Multibanco(StripeObject):
        class DisplayPreference(StripeObject):
            overridable: Optional[bool]
            """
            For child configs, whether or not the account's preference will be observed. If `false`, the parent configuration's default is used.
            """
            preference: Literal["none", "off", "on"]
            """
            The account's display preference.
            """
            value: Literal["off", "on"]
            """
            The effective display preference value.
            """

        available: bool
        """
        Whether this payment method may be offered at checkout. True if `display_preference` is `on` and the payment method's capability is active.
        """
        display_preference: DisplayPreference
        _inner_class_types = {"display_preference": DisplayPreference}

    class Oxxo(StripeObject):
        class DisplayPreference(StripeObject):
            overridable: Optional[bool]
            """
            For child configs, whether or not the account's preference will be observed. If `false`, the parent configuration's default is used.
            """
            preference: Literal["none", "off", "on"]
            """
            The account's display preference.
            """
            value: Literal["off", "on"]
            """
            The effective display preference value.
            """

        available: bool
        """
        Whether this payment method may be offered at checkout. True if `display_preference` is `on` and the payment method's capability is active.
        """
        display_preference: DisplayPreference
        _inner_class_types = {"display_preference": DisplayPreference}

    class P24(StripeObject):
        class DisplayPreference(StripeObject):
            overridable: Optional[bool]
            """
            For child configs, whether or not the account's preference will be observed. If `false`, the parent configuration's default is used.
            """
            preference: Literal["none", "off", "on"]
            """
            The account's display preference.
            """
            value: Literal["off", "on"]
            """
            The effective display preference value.
            """

        available: bool
        """
        Whether this payment method may be offered at checkout. True if `display_preference` is `on` and the payment method's capability is active.
        """
        display_preference: DisplayPreference
        _inner_class_types = {"display_preference": DisplayPreference}

    class Paynow(StripeObject):
        class DisplayPreference(StripeObject):
            overridable: Optional[bool]
            """
            For child configs, whether or not the account's preference will be observed. If `false`, the parent configuration's default is used.
            """
            preference: Literal["none", "off", "on"]
            """
            The account's display preference.
            """
            value: Literal["off", "on"]
            """
            The effective display preference value.
            """

        available: bool
        """
        Whether this payment method may be offered at checkout. True if `display_preference` is `on` and the payment method's capability is active.
        """
        display_preference: DisplayPreference
        _inner_class_types = {"display_preference": DisplayPreference}

    class Paypal(StripeObject):
        class DisplayPreference(StripeObject):
            overridable: Optional[bool]
            """
            For child configs, whether or not the account's preference will be observed. If `false`, the parent configuration's default is used.
            """
            preference: Literal["none", "off", "on"]
            """
            The account's display preference.
            """
            value: Literal["off", "on"]
            """
            The effective display preference value.
            """

        available: bool
        """
        Whether this payment method may be offered at checkout. True if `display_preference` is `on` and the payment method's capability is active.
        """
        display_preference: DisplayPreference
        _inner_class_types = {"display_preference": DisplayPreference}

    class Promptpay(StripeObject):
        class DisplayPreference(StripeObject):
            overridable: Optional[bool]
            """
            For child configs, whether or not the account's preference will be observed. If `false`, the parent configuration's default is used.
            """
            preference: Literal["none", "off", "on"]
            """
            The account's display preference.
            """
            value: Literal["off", "on"]
            """
            The effective display preference value.
            """

        available: bool
        """
        Whether this payment method may be offered at checkout. True if `display_preference` is `on` and the payment method's capability is active.
        """
        display_preference: DisplayPreference
        _inner_class_types = {"display_preference": DisplayPreference}

    class RevolutPay(StripeObject):
        class DisplayPreference(StripeObject):
            overridable: Optional[bool]
            """
            For child configs, whether or not the account's preference will be observed. If `false`, the parent configuration's default is used.
            """
            preference: Literal["none", "off", "on"]
            """
            The account's display preference.
            """
            value: Literal["off", "on"]
            """
            The effective display preference value.
            """

        available: bool
        """
        Whether this payment method may be offered at checkout. True if `display_preference` is `on` and the payment method's capability is active.
        """
        display_preference: DisplayPreference
        _inner_class_types = {"display_preference": DisplayPreference}

    class SepaDebit(StripeObject):
        class DisplayPreference(StripeObject):
            overridable: Optional[bool]
            """
            For child configs, whether or not the account's preference will be observed. If `false`, the parent configuration's default is used.
            """
            preference: Literal["none", "off", "on"]
            """
            The account's display preference.
            """
            value: Literal["off", "on"]
            """
            The effective display preference value.
            """

        available: bool
        """
        Whether this payment method may be offered at checkout. True if `display_preference` is `on` and the payment method's capability is active.
        """
        display_preference: DisplayPreference
        _inner_class_types = {"display_preference": DisplayPreference}

    class Sofort(StripeObject):
        class DisplayPreference(StripeObject):
            overridable: Optional[bool]
            """
            For child configs, whether or not the account's preference will be observed. If `false`, the parent configuration's default is used.
            """
            preference: Literal["none", "off", "on"]
            """
            The account's display preference.
            """
            value: Literal["off", "on"]
            """
            The effective display preference value.
            """

        available: bool
        """
        Whether this payment method may be offered at checkout. True if `display_preference` is `on` and the payment method's capability is active.
        """
        display_preference: DisplayPreference
        _inner_class_types = {"display_preference": DisplayPreference}

    class Swish(StripeObject):
        class DisplayPreference(StripeObject):
            overridable: Optional[bool]
            """
            For child configs, whether or not the account's preference will be observed. If `false`, the parent configuration's default is used.
            """
            preference: Literal["none", "off", "on"]
            """
            The account's display preference.
            """
            value: Literal["off", "on"]
            """
            The effective display preference value.
            """

        available: bool
        """
        Whether this payment method may be offered at checkout. True if `display_preference` is `on` and the payment method's capability is active.
        """
        display_preference: DisplayPreference
        _inner_class_types = {"display_preference": DisplayPreference}

    class Twint(StripeObject):
        class DisplayPreference(StripeObject):
            overridable: Optional[bool]
            """
            For child configs, whether or not the account's preference will be observed. If `false`, the parent configuration's default is used.
            """
            preference: Literal["none", "off", "on"]
            """
            The account's display preference.
            """
            value: Literal["off", "on"]
            """
            The effective display preference value.
            """

        available: bool
        """
        Whether this payment method may be offered at checkout. True if `display_preference` is `on` and the payment method's capability is active.
        """
        display_preference: DisplayPreference
        _inner_class_types = {"display_preference": DisplayPreference}

    class UsBankAccount(StripeObject):
        class DisplayPreference(StripeObject):
            overridable: Optional[bool]
            """
            For child configs, whether or not the account's preference will be observed. If `false`, the parent configuration's default is used.
            """
            preference: Literal["none", "off", "on"]
            """
            The account's display preference.
            """
            value: Literal["off", "on"]
            """
            The effective display preference value.
            """

        available: bool
        """
        Whether this payment method may be offered at checkout. True if `display_preference` is `on` and the payment method's capability is active.
        """
        display_preference: DisplayPreference
        _inner_class_types = {"display_preference": DisplayPreference}

    class WechatPay(StripeObject):
        class DisplayPreference(StripeObject):
            overridable: Optional[bool]
            """
            For child configs, whether or not the account's preference will be observed. If `false`, the parent configuration's default is used.
            """
            preference: Literal["none", "off", "on"]
            """
            The account's display preference.
            """
            value: Literal["off", "on"]
            """
            The effective display preference value.
            """

        available: bool
        """
        Whether this payment method may be offered at checkout. True if `display_preference` is `on` and the payment method's capability is active.
        """
        display_preference: DisplayPreference
        _inner_class_types = {"display_preference": DisplayPreference}

    class Zip(StripeObject):
        class DisplayPreference(StripeObject):
            overridable: Optional[bool]
            """
            For child configs, whether or not the account's preference will be observed. If `false`, the parent configuration's default is used.
            """
            preference: Literal["none", "off", "on"]
            """
            The account's display preference.
            """
            value: Literal["off", "on"]
            """
            The effective display preference value.
            """

        available: bool
        """
        Whether this payment method may be offered at checkout. True if `display_preference` is `on` and the payment method's capability is active.
        """
        display_preference: DisplayPreference
        _inner_class_types = {"display_preference": DisplayPreference}

    class CreateParams(RequestOptions):
        acss_debit: NotRequired[
            "PaymentMethodConfiguration.CreateParamsAcssDebit"
        ]
        """
        Canadian pre-authorized debit payments, check this [page](https://stripe.com/docs/payments/acss-debit) for more details like country availability.
        """
        affirm: NotRequired["PaymentMethodConfiguration.CreateParamsAffirm"]
        """
        [Affirm](https://www.affirm.com/) gives your customers a way to split purchases over a series of payments. Depending on the purchase, they can pay with four interest-free payments (Split Pay) or pay over a longer term (Installments), which might include interest. Check this [page](https://stripe.com/docs/payments/affirm) for more details like country availability.
        """
        afterpay_clearpay: NotRequired[
            "PaymentMethodConfiguration.CreateParamsAfterpayClearpay"
        ]
        """
        Afterpay gives your customers a way to pay for purchases in installments, check this [page](https://stripe.com/docs/payments/afterpay-clearpay) for more details like country availability. Afterpay is particularly popular among businesses selling fashion, beauty, and sports products.
        """
        alipay: NotRequired["PaymentMethodConfiguration.CreateParamsAlipay"]
        """
        Alipay is a digital wallet in China that has more than a billion active users worldwide. Alipay users can pay on the web or on a mobile device using login credentials or their Alipay app. Alipay has a low dispute rate and reduces fraud by authenticating payments using the customer's login credentials. Check this [page](https://stripe.com/docs/payments/alipay) for more details.
        """
        amazon_pay: NotRequired[
            "PaymentMethodConfiguration.CreateParamsAmazonPay"
        ]
        """
        Amazon Pay is a wallet payment method that lets your customers check out the same way as on Amazon.
        """
        apple_pay: NotRequired[
            "PaymentMethodConfiguration.CreateParamsApplePay"
        ]
        """
        Stripe users can accept [Apple Pay](https://stripe.com/payments/apple-pay) in iOS applications in iOS 9 and later, and on the web in Safari starting with iOS 10 or macOS Sierra. There are no additional fees to process Apple Pay payments, and the [pricing](https://stripe.com/pricing) is the same as other card transactions. Check this [page](https://stripe.com/docs/apple-pay) for more details.
        """
        apple_pay_later: NotRequired[
            "PaymentMethodConfiguration.CreateParamsApplePayLater"
        ]
        """
        Apple Pay Later, a payment method for customers to buy now and pay later, gives your customers a way to split purchases into four installments across six weeks.
        """
        au_becs_debit: NotRequired[
            "PaymentMethodConfiguration.CreateParamsAuBecsDebit"
        ]
        """
        Stripe users in Australia can accept Bulk Electronic Clearing System (BECS) direct debit payments from customers with an Australian bank account. Check this [page](https://stripe.com/docs/payments/au-becs-debit) for more details.
        """
        bacs_debit: NotRequired[
            "PaymentMethodConfiguration.CreateParamsBacsDebit"
        ]
        """
        Stripe users in the UK can accept Bacs Direct Debit payments from customers with a UK bank account, check this [page](https://stripe.com/docs/payments/payment-methods/bacs-debit) for more details.
        """
        bancontact: NotRequired[
            "PaymentMethodConfiguration.CreateParamsBancontact"
        ]
        """
        Bancontact is the most popular online payment method in Belgium, with over 15 million cards in circulation. [Customers](https://stripe.com/docs/api/customers) use a Bancontact card or mobile app linked to a Belgian bank account to make online payments that are secure, guaranteed, and confirmed immediately. Check this [page](https://stripe.com/docs/payments/bancontact) for more details.
        """
        blik: NotRequired["PaymentMethodConfiguration.CreateParamsBlik"]
        """
        BLIK is a [single use](https://stripe.com/docs/payments/payment-methods#usage) payment method that requires customers to authenticate their payments. When customers want to pay online using BLIK, they request a six-digit code from their banking application and enter it into the payment collection form. Check this [page](https://stripe.com/docs/payments/blik) for more details.
        """
        boleto: NotRequired["PaymentMethodConfiguration.CreateParamsBoleto"]
        """
        Boleto is an official (regulated by the Central Bank of Brazil) payment method in Brazil. Check this [page](https://stripe.com/docs/payments/boleto) for more details.
        """
        card: NotRequired["PaymentMethodConfiguration.CreateParamsCard"]
        """
        Cards are a popular way for consumers and businesses to pay online or in person. Stripe supports global and local card networks.
        """
        cartes_bancaires: NotRequired[
            "PaymentMethodConfiguration.CreateParamsCartesBancaires"
        ]
        """
        Cartes Bancaires is France's local card network. More than 95% of these cards are co-branded with either Visa or Mastercard, meaning you can process these cards over either Cartes Bancaires or the Visa or Mastercard networks. Check this [page](https://stripe.com/docs/payments/cartes-bancaires) for more details.
        """
        cashapp: NotRequired["PaymentMethodConfiguration.CreateParamsCashapp"]
        """
        Cash App is a popular consumer app in the US that allows customers to bank, invest, send, and receive money using their digital wallet. Check this [page](https://stripe.com/docs/payments/cash-app-pay) for more details.
        """
        customer_balance: NotRequired[
            "PaymentMethodConfiguration.CreateParamsCustomerBalance"
        ]
        """
        Uses a customer's [cash balance](https://stripe.com/docs/payments/customer-balance) for the payment. The cash balance can be funded via a bank transfer. Check this [page](https://stripe.com/docs/payments/bank-transfers) for more details.
        """
        eps: NotRequired["PaymentMethodConfiguration.CreateParamsEps"]
        """
        EPS is an Austria-based payment method that allows customers to complete transactions online using their bank credentials. EPS is supported by all Austrian banks and is accepted by over 80% of Austrian online retailers. Check this [page](https://stripe.com/docs/payments/eps) for more details.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        fpx: NotRequired["PaymentMethodConfiguration.CreateParamsFpx"]
        """
        Financial Process Exchange (FPX) is a Malaysia-based payment method that allows customers to complete transactions online using their bank credentials. Bank Negara Malaysia (BNM), the Central Bank of Malaysia, and eleven other major Malaysian financial institutions are members of the PayNet Group, which owns and operates FPX. It is one of the most popular online payment methods in Malaysia, with nearly 90 million transactions in 2018 according to BNM. Check this [page](https://stripe.com/docs/payments/fpx) for more details.
        """
        giropay: NotRequired["PaymentMethodConfiguration.CreateParamsGiropay"]
        """
        giropay is a German payment method based on online banking, introduced in 2006. It allows customers to complete transactions online using their online banking environment, with funds debited from their bank account. Depending on their bank, customers confirm payments on giropay using a second factor of authentication or a PIN. giropay accounts for 10% of online checkouts in Germany. Check this [page](https://stripe.com/docs/payments/giropay) for more details.
        """
        google_pay: NotRequired[
            "PaymentMethodConfiguration.CreateParamsGooglePay"
        ]
        """
        Google Pay allows customers to make payments in your app or website using any credit or debit card saved to their Google Account, including those from Google Play, YouTube, Chrome, or an Android device. Use the Google Pay API to request any credit or debit card stored in your customer's Google account. Check this [page](https://stripe.com/docs/google-pay) for more details.
        """
        grabpay: NotRequired["PaymentMethodConfiguration.CreateParamsGrabpay"]
        """
        GrabPay is a payment method developed by [Grab](https://www.grab.com/sg/consumer/finance/pay/). GrabPay is a digital wallet - customers maintain a balance in their wallets that they pay out with. Check this [page](https://stripe.com/docs/payments/grabpay) for more details.
        """
        ideal: NotRequired["PaymentMethodConfiguration.CreateParamsIdeal"]
        """
        iDEAL is a Netherlands-based payment method that allows customers to complete transactions online using their bank credentials. All major Dutch banks are members of Currence, the scheme that operates iDEAL, making it the most popular online payment method in the Netherlands with a share of online transactions close to 55%. Check this [page](https://stripe.com/docs/payments/ideal) for more details.
        """
        jcb: NotRequired["PaymentMethodConfiguration.CreateParamsJcb"]
        """
        JCB is a credit card company based in Japan. JCB is currently available in Japan to businesses approved by JCB, and available to all businesses in Australia, Canada, Hong Kong, Japan, New Zealand, Singapore, Switzerland, United Kingdom, United States, and all countries in the European Economic Area except Iceland. Check this [page](https://support.stripe.com/questions/accepting-japan-credit-bureau-%28jcb%29-payments) for more details.
        """
        klarna: NotRequired["PaymentMethodConfiguration.CreateParamsKlarna"]
        """
        Klarna gives customers a range of [payment options](https://stripe.com/docs/payments/klarna#payment-options) during checkout. Available payment options vary depending on the customer's billing address and the transaction amount. These payment options make it convenient for customers to purchase items in all price ranges. Check this [page](https://stripe.com/docs/payments/klarna) for more details.
        """
        konbini: NotRequired["PaymentMethodConfiguration.CreateParamsKonbini"]
        """
        Konbini allows customers in Japan to pay for bills and online purchases at convenience stores with cash. Check this [page](https://stripe.com/docs/payments/konbini) for more details.
        """
        link: NotRequired["PaymentMethodConfiguration.CreateParamsLink"]
        """
        [Link](https://stripe.com/docs/payments/link) is a payment method network. With Link, users save their payment details once, then reuse that information to pay with one click for any business on the network.
        """
        mobilepay: NotRequired[
            "PaymentMethodConfiguration.CreateParamsMobilepay"
        ]
        """
        MobilePay is a [single-use](https://stripe.com/docs/payments/payment-methods#usage) card wallet payment method used in Denmark and Finland. It allows customers to [authenticate and approve](https://stripe.com/docs/payments/payment-methods#customer-actions) payments using the MobilePay app. Check this [page](https://stripe.com/docs/payments/mobilepay) for more details.
        """
        multibanco: NotRequired[
            "PaymentMethodConfiguration.CreateParamsMultibanco"
        ]
        """
        Stripe users in Europe and the United States can accept Multibanco payments from customers in Portugal using [Sources](https://stripe.com/docs/sources)—a single integration path for creating payments using any supported method.
        """
        name: NotRequired[str]
        """
        Configuration name.
        """
        oxxo: NotRequired["PaymentMethodConfiguration.CreateParamsOxxo"]
        """
        OXXO is a Mexican chain of convenience stores with thousands of locations across Latin America and represents nearly 20% of online transactions in Mexico. OXXO allows customers to pay bills and online purchases in-store with cash. Check this [page](https://stripe.com/docs/payments/oxxo) for more details.
        """
        p24: NotRequired["PaymentMethodConfiguration.CreateParamsP24"]
        """
        Przelewy24 is a Poland-based payment method aggregator that allows customers to complete transactions online using bank transfers and other methods. Bank transfers account for 30% of online payments in Poland and Przelewy24 provides a way for customers to pay with over 165 banks. Check this [page](https://stripe.com/docs/payments/p24) for more details.
        """
        parent: NotRequired[str]
        """
        Configuration's parent configuration. Specify to create a child configuration.
        """
        paynow: NotRequired["PaymentMethodConfiguration.CreateParamsPaynow"]
        """
        PayNow is a Singapore-based payment method that allows customers to make a payment using their preferred app from participating banks and participating non-bank financial institutions. Check this [page](https://stripe.com/docs/payments/paynow) for more details.
        """
        paypal: NotRequired["PaymentMethodConfiguration.CreateParamsPaypal"]
        """
        PayPal, a digital wallet popular with customers in Europe, allows your customers worldwide to pay using their PayPal account. Check this [page](https://stripe.com/docs/payments/paypal) for more details.
        """
        promptpay: NotRequired[
            "PaymentMethodConfiguration.CreateParamsPromptpay"
        ]
        """
        PromptPay is a Thailand-based payment method that allows customers to make a payment using their preferred app from participating banks. Check this [page](https://stripe.com/docs/payments/promptpay) for more details.
        """
        revolut_pay: NotRequired[
            "PaymentMethodConfiguration.CreateParamsRevolutPay"
        ]
        """
        Revolut Pay, developed by Revolut, a global finance app, is a digital wallet payment method. Revolut Pay uses the customer's stored balance or cards to fund the payment, and offers the option for non-Revolut customers to save their details after their first purchase.
        """
        sepa_debit: NotRequired[
            "PaymentMethodConfiguration.CreateParamsSepaDebit"
        ]
        """
        The [Single Euro Payments Area (SEPA)](https://en.wikipedia.org/wiki/Single_Euro_Payments_Area) is an initiative of the European Union to simplify payments within and across member countries. SEPA established and enforced banking standards to allow for the direct debiting of every EUR-denominated bank account within the SEPA region, check this [page](https://stripe.com/docs/payments/sepa-debit) for more details.
        """
        sofort: NotRequired["PaymentMethodConfiguration.CreateParamsSofort"]
        """
        Stripe users in Europe and the United States can use the [Payment Intents API](https://stripe.com/docs/payments/payment-intents)—a single integration path for creating payments using any supported method—to accept [Sofort](https://www.sofort.com/) payments from customers. Check this [page](https://stripe.com/docs/payments/sofort) for more details.
        """
        swish: NotRequired["PaymentMethodConfiguration.CreateParamsSwish"]
        """
        Swish is a [real-time](https://stripe.com/docs/payments/real-time) payment method popular in Sweden. It allows customers to [authenticate and approve](https://stripe.com/docs/payments/payment-methods#customer-actions) payments using the Swish mobile app and the Swedish BankID mobile app. Check this [page](https://stripe.com/docs/payments/swish) for more details.
        """
        twint: NotRequired["PaymentMethodConfiguration.CreateParamsTwint"]
        """
        Twint is a payment method popular in Switzerland. It allows customers to pay using their mobile phone. Check this [page](https://docs.stripe.com/payments/twint) for more details.
        """
        us_bank_account: NotRequired[
            "PaymentMethodConfiguration.CreateParamsUsBankAccount"
        ]
        """
        Stripe users in the United States can accept ACH direct debit payments from customers with a US bank account using the Automated Clearing House (ACH) payments system operated by Nacha. Check this [page](https://stripe.com/docs/payments/ach-debit) for more details.
        """
        wechat_pay: NotRequired[
            "PaymentMethodConfiguration.CreateParamsWechatPay"
        ]
        """
        WeChat, owned by Tencent, is China's leading mobile app with over 1 billion monthly active users. Chinese consumers can use WeChat Pay to pay for goods and services inside of businesses' apps and websites. WeChat Pay users buy most frequently in gaming, e-commerce, travel, online education, and food/nutrition. Check this [page](https://stripe.com/docs/payments/wechat-pay) for more details.
        """
        zip: NotRequired["PaymentMethodConfiguration.CreateParamsZip"]
        """
        Zip gives your customers a way to split purchases over a series of payments. Check this [page](https://stripe.com/docs/payments/zip) for more details like country availability.
        """

    class CreateParamsAcssDebit(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfiguration.CreateParamsAcssDebitDisplayPreference"
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
            "PaymentMethodConfiguration.CreateParamsAffirmDisplayPreference"
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
            "PaymentMethodConfiguration.CreateParamsAfterpayClearpayDisplayPreference"
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
            "PaymentMethodConfiguration.CreateParamsAlipayDisplayPreference"
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
            "PaymentMethodConfiguration.CreateParamsAmazonPayDisplayPreference"
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
            "PaymentMethodConfiguration.CreateParamsApplePayDisplayPreference"
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
            "PaymentMethodConfiguration.CreateParamsApplePayLaterDisplayPreference"
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
            "PaymentMethodConfiguration.CreateParamsAuBecsDebitDisplayPreference"
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
            "PaymentMethodConfiguration.CreateParamsBacsDebitDisplayPreference"
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
            "PaymentMethodConfiguration.CreateParamsBancontactDisplayPreference"
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
            "PaymentMethodConfiguration.CreateParamsBlikDisplayPreference"
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
            "PaymentMethodConfiguration.CreateParamsBoletoDisplayPreference"
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
            "PaymentMethodConfiguration.CreateParamsCardDisplayPreference"
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
            "PaymentMethodConfiguration.CreateParamsCartesBancairesDisplayPreference"
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
            "PaymentMethodConfiguration.CreateParamsCashappDisplayPreference"
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
            "PaymentMethodConfiguration.CreateParamsCustomerBalanceDisplayPreference"
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
            "PaymentMethodConfiguration.CreateParamsEpsDisplayPreference"
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
            "PaymentMethodConfiguration.CreateParamsFpxDisplayPreference"
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
            "PaymentMethodConfiguration.CreateParamsGiropayDisplayPreference"
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
            "PaymentMethodConfiguration.CreateParamsGooglePayDisplayPreference"
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
            "PaymentMethodConfiguration.CreateParamsGrabpayDisplayPreference"
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
            "PaymentMethodConfiguration.CreateParamsIdealDisplayPreference"
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
            "PaymentMethodConfiguration.CreateParamsJcbDisplayPreference"
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
            "PaymentMethodConfiguration.CreateParamsKlarnaDisplayPreference"
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
            "PaymentMethodConfiguration.CreateParamsKonbiniDisplayPreference"
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
            "PaymentMethodConfiguration.CreateParamsLinkDisplayPreference"
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
            "PaymentMethodConfiguration.CreateParamsMobilepayDisplayPreference"
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
            "PaymentMethodConfiguration.CreateParamsMultibancoDisplayPreference"
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
            "PaymentMethodConfiguration.CreateParamsOxxoDisplayPreference"
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
            "PaymentMethodConfiguration.CreateParamsP24DisplayPreference"
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
            "PaymentMethodConfiguration.CreateParamsPaynowDisplayPreference"
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
            "PaymentMethodConfiguration.CreateParamsPaypalDisplayPreference"
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
            "PaymentMethodConfiguration.CreateParamsPromptpayDisplayPreference"
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
            "PaymentMethodConfiguration.CreateParamsRevolutPayDisplayPreference"
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
            "PaymentMethodConfiguration.CreateParamsSepaDebitDisplayPreference"
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
            "PaymentMethodConfiguration.CreateParamsSofortDisplayPreference"
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
            "PaymentMethodConfiguration.CreateParamsSwishDisplayPreference"
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
            "PaymentMethodConfiguration.CreateParamsTwintDisplayPreference"
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
            "PaymentMethodConfiguration.CreateParamsUsBankAccountDisplayPreference"
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
            "PaymentMethodConfiguration.CreateParamsWechatPayDisplayPreference"
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
            "PaymentMethodConfiguration.CreateParamsZipDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class CreateParamsZipDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class ListParams(RequestOptions):
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

    class ModifyParams(RequestOptions):
        acss_debit: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsAcssDebit"
        ]
        """
        Canadian pre-authorized debit payments, check this [page](https://stripe.com/docs/payments/acss-debit) for more details like country availability.
        """
        active: NotRequired[bool]
        """
        Whether the configuration can be used for new payments.
        """
        affirm: NotRequired["PaymentMethodConfiguration.ModifyParamsAffirm"]
        """
        [Affirm](https://www.affirm.com/) gives your customers a way to split purchases over a series of payments. Depending on the purchase, they can pay with four interest-free payments (Split Pay) or pay over a longer term (Installments), which might include interest. Check this [page](https://stripe.com/docs/payments/affirm) for more details like country availability.
        """
        afterpay_clearpay: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsAfterpayClearpay"
        ]
        """
        Afterpay gives your customers a way to pay for purchases in installments, check this [page](https://stripe.com/docs/payments/afterpay-clearpay) for more details like country availability. Afterpay is particularly popular among businesses selling fashion, beauty, and sports products.
        """
        alipay: NotRequired["PaymentMethodConfiguration.ModifyParamsAlipay"]
        """
        Alipay is a digital wallet in China that has more than a billion active users worldwide. Alipay users can pay on the web or on a mobile device using login credentials or their Alipay app. Alipay has a low dispute rate and reduces fraud by authenticating payments using the customer's login credentials. Check this [page](https://stripe.com/docs/payments/alipay) for more details.
        """
        amazon_pay: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsAmazonPay"
        ]
        """
        Amazon Pay is a wallet payment method that lets your customers check out the same way as on Amazon.
        """
        apple_pay: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsApplePay"
        ]
        """
        Stripe users can accept [Apple Pay](https://stripe.com/payments/apple-pay) in iOS applications in iOS 9 and later, and on the web in Safari starting with iOS 10 or macOS Sierra. There are no additional fees to process Apple Pay payments, and the [pricing](https://stripe.com/pricing) is the same as other card transactions. Check this [page](https://stripe.com/docs/apple-pay) for more details.
        """
        apple_pay_later: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsApplePayLater"
        ]
        """
        Apple Pay Later, a payment method for customers to buy now and pay later, gives your customers a way to split purchases into four installments across six weeks.
        """
        au_becs_debit: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsAuBecsDebit"
        ]
        """
        Stripe users in Australia can accept Bulk Electronic Clearing System (BECS) direct debit payments from customers with an Australian bank account. Check this [page](https://stripe.com/docs/payments/au-becs-debit) for more details.
        """
        bacs_debit: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsBacsDebit"
        ]
        """
        Stripe users in the UK can accept Bacs Direct Debit payments from customers with a UK bank account, check this [page](https://stripe.com/docs/payments/payment-methods/bacs-debit) for more details.
        """
        bancontact: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsBancontact"
        ]
        """
        Bancontact is the most popular online payment method in Belgium, with over 15 million cards in circulation. [Customers](https://stripe.com/docs/api/customers) use a Bancontact card or mobile app linked to a Belgian bank account to make online payments that are secure, guaranteed, and confirmed immediately. Check this [page](https://stripe.com/docs/payments/bancontact) for more details.
        """
        blik: NotRequired["PaymentMethodConfiguration.ModifyParamsBlik"]
        """
        BLIK is a [single use](https://stripe.com/docs/payments/payment-methods#usage) payment method that requires customers to authenticate their payments. When customers want to pay online using BLIK, they request a six-digit code from their banking application and enter it into the payment collection form. Check this [page](https://stripe.com/docs/payments/blik) for more details.
        """
        boleto: NotRequired["PaymentMethodConfiguration.ModifyParamsBoleto"]
        """
        Boleto is an official (regulated by the Central Bank of Brazil) payment method in Brazil. Check this [page](https://stripe.com/docs/payments/boleto) for more details.
        """
        card: NotRequired["PaymentMethodConfiguration.ModifyParamsCard"]
        """
        Cards are a popular way for consumers and businesses to pay online or in person. Stripe supports global and local card networks.
        """
        cartes_bancaires: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsCartesBancaires"
        ]
        """
        Cartes Bancaires is France's local card network. More than 95% of these cards are co-branded with either Visa or Mastercard, meaning you can process these cards over either Cartes Bancaires or the Visa or Mastercard networks. Check this [page](https://stripe.com/docs/payments/cartes-bancaires) for more details.
        """
        cashapp: NotRequired["PaymentMethodConfiguration.ModifyParamsCashapp"]
        """
        Cash App is a popular consumer app in the US that allows customers to bank, invest, send, and receive money using their digital wallet. Check this [page](https://stripe.com/docs/payments/cash-app-pay) for more details.
        """
        customer_balance: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsCustomerBalance"
        ]
        """
        Uses a customer's [cash balance](https://stripe.com/docs/payments/customer-balance) for the payment. The cash balance can be funded via a bank transfer. Check this [page](https://stripe.com/docs/payments/bank-transfers) for more details.
        """
        eps: NotRequired["PaymentMethodConfiguration.ModifyParamsEps"]
        """
        EPS is an Austria-based payment method that allows customers to complete transactions online using their bank credentials. EPS is supported by all Austrian banks and is accepted by over 80% of Austrian online retailers. Check this [page](https://stripe.com/docs/payments/eps) for more details.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        fpx: NotRequired["PaymentMethodConfiguration.ModifyParamsFpx"]
        """
        Financial Process Exchange (FPX) is a Malaysia-based payment method that allows customers to complete transactions online using their bank credentials. Bank Negara Malaysia (BNM), the Central Bank of Malaysia, and eleven other major Malaysian financial institutions are members of the PayNet Group, which owns and operates FPX. It is one of the most popular online payment methods in Malaysia, with nearly 90 million transactions in 2018 according to BNM. Check this [page](https://stripe.com/docs/payments/fpx) for more details.
        """
        giropay: NotRequired["PaymentMethodConfiguration.ModifyParamsGiropay"]
        """
        giropay is a German payment method based on online banking, introduced in 2006. It allows customers to complete transactions online using their online banking environment, with funds debited from their bank account. Depending on their bank, customers confirm payments on giropay using a second factor of authentication or a PIN. giropay accounts for 10% of online checkouts in Germany. Check this [page](https://stripe.com/docs/payments/giropay) for more details.
        """
        google_pay: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsGooglePay"
        ]
        """
        Google Pay allows customers to make payments in your app or website using any credit or debit card saved to their Google Account, including those from Google Play, YouTube, Chrome, or an Android device. Use the Google Pay API to request any credit or debit card stored in your customer's Google account. Check this [page](https://stripe.com/docs/google-pay) for more details.
        """
        grabpay: NotRequired["PaymentMethodConfiguration.ModifyParamsGrabpay"]
        """
        GrabPay is a payment method developed by [Grab](https://www.grab.com/sg/consumer/finance/pay/). GrabPay is a digital wallet - customers maintain a balance in their wallets that they pay out with. Check this [page](https://stripe.com/docs/payments/grabpay) for more details.
        """
        ideal: NotRequired["PaymentMethodConfiguration.ModifyParamsIdeal"]
        """
        iDEAL is a Netherlands-based payment method that allows customers to complete transactions online using their bank credentials. All major Dutch banks are members of Currence, the scheme that operates iDEAL, making it the most popular online payment method in the Netherlands with a share of online transactions close to 55%. Check this [page](https://stripe.com/docs/payments/ideal) for more details.
        """
        jcb: NotRequired["PaymentMethodConfiguration.ModifyParamsJcb"]
        """
        JCB is a credit card company based in Japan. JCB is currently available in Japan to businesses approved by JCB, and available to all businesses in Australia, Canada, Hong Kong, Japan, New Zealand, Singapore, Switzerland, United Kingdom, United States, and all countries in the European Economic Area except Iceland. Check this [page](https://support.stripe.com/questions/accepting-japan-credit-bureau-%28jcb%29-payments) for more details.
        """
        klarna: NotRequired["PaymentMethodConfiguration.ModifyParamsKlarna"]
        """
        Klarna gives customers a range of [payment options](https://stripe.com/docs/payments/klarna#payment-options) during checkout. Available payment options vary depending on the customer's billing address and the transaction amount. These payment options make it convenient for customers to purchase items in all price ranges. Check this [page](https://stripe.com/docs/payments/klarna) for more details.
        """
        konbini: NotRequired["PaymentMethodConfiguration.ModifyParamsKonbini"]
        """
        Konbini allows customers in Japan to pay for bills and online purchases at convenience stores with cash. Check this [page](https://stripe.com/docs/payments/konbini) for more details.
        """
        link: NotRequired["PaymentMethodConfiguration.ModifyParamsLink"]
        """
        [Link](https://stripe.com/docs/payments/link) is a payment method network. With Link, users save their payment details once, then reuse that information to pay with one click for any business on the network.
        """
        mobilepay: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsMobilepay"
        ]
        """
        MobilePay is a [single-use](https://stripe.com/docs/payments/payment-methods#usage) card wallet payment method used in Denmark and Finland. It allows customers to [authenticate and approve](https://stripe.com/docs/payments/payment-methods#customer-actions) payments using the MobilePay app. Check this [page](https://stripe.com/docs/payments/mobilepay) for more details.
        """
        multibanco: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsMultibanco"
        ]
        """
        Stripe users in Europe and the United States can accept Multibanco payments from customers in Portugal using [Sources](https://stripe.com/docs/sources)—a single integration path for creating payments using any supported method.
        """
        name: NotRequired[str]
        """
        Configuration name.
        """
        oxxo: NotRequired["PaymentMethodConfiguration.ModifyParamsOxxo"]
        """
        OXXO is a Mexican chain of convenience stores with thousands of locations across Latin America and represents nearly 20% of online transactions in Mexico. OXXO allows customers to pay bills and online purchases in-store with cash. Check this [page](https://stripe.com/docs/payments/oxxo) for more details.
        """
        p24: NotRequired["PaymentMethodConfiguration.ModifyParamsP24"]
        """
        Przelewy24 is a Poland-based payment method aggregator that allows customers to complete transactions online using bank transfers and other methods. Bank transfers account for 30% of online payments in Poland and Przelewy24 provides a way for customers to pay with over 165 banks. Check this [page](https://stripe.com/docs/payments/p24) for more details.
        """
        paynow: NotRequired["PaymentMethodConfiguration.ModifyParamsPaynow"]
        """
        PayNow is a Singapore-based payment method that allows customers to make a payment using their preferred app from participating banks and participating non-bank financial institutions. Check this [page](https://stripe.com/docs/payments/paynow) for more details.
        """
        paypal: NotRequired["PaymentMethodConfiguration.ModifyParamsPaypal"]
        """
        PayPal, a digital wallet popular with customers in Europe, allows your customers worldwide to pay using their PayPal account. Check this [page](https://stripe.com/docs/payments/paypal) for more details.
        """
        promptpay: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsPromptpay"
        ]
        """
        PromptPay is a Thailand-based payment method that allows customers to make a payment using their preferred app from participating banks. Check this [page](https://stripe.com/docs/payments/promptpay) for more details.
        """
        revolut_pay: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsRevolutPay"
        ]
        """
        Revolut Pay, developed by Revolut, a global finance app, is a digital wallet payment method. Revolut Pay uses the customer's stored balance or cards to fund the payment, and offers the option for non-Revolut customers to save their details after their first purchase.
        """
        sepa_debit: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsSepaDebit"
        ]
        """
        The [Single Euro Payments Area (SEPA)](https://en.wikipedia.org/wiki/Single_Euro_Payments_Area) is an initiative of the European Union to simplify payments within and across member countries. SEPA established and enforced banking standards to allow for the direct debiting of every EUR-denominated bank account within the SEPA region, check this [page](https://stripe.com/docs/payments/sepa-debit) for more details.
        """
        sofort: NotRequired["PaymentMethodConfiguration.ModifyParamsSofort"]
        """
        Stripe users in Europe and the United States can use the [Payment Intents API](https://stripe.com/docs/payments/payment-intents)—a single integration path for creating payments using any supported method—to accept [Sofort](https://www.sofort.com/) payments from customers. Check this [page](https://stripe.com/docs/payments/sofort) for more details.
        """
        swish: NotRequired["PaymentMethodConfiguration.ModifyParamsSwish"]
        """
        Swish is a [real-time](https://stripe.com/docs/payments/real-time) payment method popular in Sweden. It allows customers to [authenticate and approve](https://stripe.com/docs/payments/payment-methods#customer-actions) payments using the Swish mobile app and the Swedish BankID mobile app. Check this [page](https://stripe.com/docs/payments/swish) for more details.
        """
        twint: NotRequired["PaymentMethodConfiguration.ModifyParamsTwint"]
        """
        Twint is a payment method popular in Switzerland. It allows customers to pay using their mobile phone. Check this [page](https://docs.stripe.com/payments/twint) for more details.
        """
        us_bank_account: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsUsBankAccount"
        ]
        """
        Stripe users in the United States can accept ACH direct debit payments from customers with a US bank account using the Automated Clearing House (ACH) payments system operated by Nacha. Check this [page](https://stripe.com/docs/payments/ach-debit) for more details.
        """
        wechat_pay: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsWechatPay"
        ]
        """
        WeChat, owned by Tencent, is China's leading mobile app with over 1 billion monthly active users. Chinese consumers can use WeChat Pay to pay for goods and services inside of businesses' apps and websites. WeChat Pay users buy most frequently in gaming, e-commerce, travel, online education, and food/nutrition. Check this [page](https://stripe.com/docs/payments/wechat-pay) for more details.
        """
        zip: NotRequired["PaymentMethodConfiguration.ModifyParamsZip"]
        """
        Zip gives your customers a way to split purchases over a series of payments. Check this [page](https://stripe.com/docs/payments/zip) for more details like country availability.
        """

    class ModifyParamsAcssDebit(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsAcssDebitDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class ModifyParamsAcssDebitDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class ModifyParamsAffirm(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsAffirmDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class ModifyParamsAffirmDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class ModifyParamsAfterpayClearpay(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsAfterpayClearpayDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class ModifyParamsAfterpayClearpayDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class ModifyParamsAlipay(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsAlipayDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class ModifyParamsAlipayDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class ModifyParamsAmazonPay(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsAmazonPayDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class ModifyParamsAmazonPayDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class ModifyParamsApplePay(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsApplePayDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class ModifyParamsApplePayDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class ModifyParamsApplePayLater(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsApplePayLaterDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class ModifyParamsApplePayLaterDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class ModifyParamsAuBecsDebit(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsAuBecsDebitDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class ModifyParamsAuBecsDebitDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class ModifyParamsBacsDebit(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsBacsDebitDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class ModifyParamsBacsDebitDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class ModifyParamsBancontact(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsBancontactDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class ModifyParamsBancontactDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class ModifyParamsBlik(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsBlikDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class ModifyParamsBlikDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class ModifyParamsBoleto(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsBoletoDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class ModifyParamsBoletoDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class ModifyParamsCard(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsCardDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class ModifyParamsCardDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class ModifyParamsCartesBancaires(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsCartesBancairesDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class ModifyParamsCartesBancairesDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class ModifyParamsCashapp(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsCashappDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class ModifyParamsCashappDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class ModifyParamsCustomerBalance(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsCustomerBalanceDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class ModifyParamsCustomerBalanceDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class ModifyParamsEps(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsEpsDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class ModifyParamsEpsDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class ModifyParamsFpx(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsFpxDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class ModifyParamsFpxDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class ModifyParamsGiropay(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsGiropayDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class ModifyParamsGiropayDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class ModifyParamsGooglePay(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsGooglePayDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class ModifyParamsGooglePayDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class ModifyParamsGrabpay(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsGrabpayDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class ModifyParamsGrabpayDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class ModifyParamsIdeal(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsIdealDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class ModifyParamsIdealDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class ModifyParamsJcb(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsJcbDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class ModifyParamsJcbDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class ModifyParamsKlarna(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsKlarnaDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class ModifyParamsKlarnaDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class ModifyParamsKonbini(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsKonbiniDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class ModifyParamsKonbiniDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class ModifyParamsLink(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsLinkDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class ModifyParamsLinkDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class ModifyParamsMobilepay(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsMobilepayDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class ModifyParamsMobilepayDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class ModifyParamsMultibanco(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsMultibancoDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class ModifyParamsMultibancoDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class ModifyParamsOxxo(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsOxxoDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class ModifyParamsOxxoDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class ModifyParamsP24(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsP24DisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class ModifyParamsP24DisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class ModifyParamsPaynow(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsPaynowDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class ModifyParamsPaynowDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class ModifyParamsPaypal(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsPaypalDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class ModifyParamsPaypalDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class ModifyParamsPromptpay(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsPromptpayDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class ModifyParamsPromptpayDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class ModifyParamsRevolutPay(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsRevolutPayDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class ModifyParamsRevolutPayDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class ModifyParamsSepaDebit(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsSepaDebitDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class ModifyParamsSepaDebitDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class ModifyParamsSofort(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsSofortDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class ModifyParamsSofortDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class ModifyParamsSwish(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsSwishDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class ModifyParamsSwishDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class ModifyParamsTwint(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsTwintDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class ModifyParamsTwintDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class ModifyParamsUsBankAccount(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsUsBankAccountDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class ModifyParamsUsBankAccountDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class ModifyParamsWechatPay(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsWechatPayDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class ModifyParamsWechatPayDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class ModifyParamsZip(TypedDict):
        display_preference: NotRequired[
            "PaymentMethodConfiguration.ModifyParamsZipDisplayPreference"
        ]
        """
        Whether or not the payment method should be displayed.
        """

    class ModifyParamsZipDisplayPreference(TypedDict):
        preference: NotRequired[Literal["none", "off", "on"]]
        """
        The account's preference for whether or not to display this payment method.
        """

    class RetrieveParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    acss_debit: Optional[AcssDebit]
    active: bool
    """
    Whether the configuration can be used for new payments.
    """
    affirm: Optional[Affirm]
    afterpay_clearpay: Optional[AfterpayClearpay]
    alipay: Optional[Alipay]
    amazon_pay: Optional[AmazonPay]
    apple_pay: Optional[ApplePay]
    application: Optional[str]
    """
    For child configs, the Connect application associated with the configuration.
    """
    au_becs_debit: Optional[AuBecsDebit]
    bacs_debit: Optional[BacsDebit]
    bancontact: Optional[Bancontact]
    blik: Optional[Blik]
    boleto: Optional[Boleto]
    card: Optional[Card]
    cartes_bancaires: Optional[CartesBancaires]
    cashapp: Optional[Cashapp]
    customer_balance: Optional[CustomerBalance]
    eps: Optional[Eps]
    fpx: Optional[Fpx]
    giropay: Optional[Giropay]
    google_pay: Optional[GooglePay]
    grabpay: Optional[Grabpay]
    id: str
    """
    Unique identifier for the object.
    """
    ideal: Optional[Ideal]
    is_default: bool
    """
    The default configuration is used whenever a payment method configuration is not specified.
    """
    jcb: Optional[Jcb]
    klarna: Optional[Klarna]
    konbini: Optional[Konbini]
    link: Optional[Link]
    livemode: bool
    """
    Has the value `true` if the object exists in live mode or the value `false` if the object exists in test mode.
    """
    mobilepay: Optional[Mobilepay]
    multibanco: Optional[Multibanco]
    name: str
    """
    The configuration's name.
    """
    object: Literal["payment_method_configuration"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """
    oxxo: Optional[Oxxo]
    p24: Optional[P24]
    parent: Optional[str]
    """
    For child configs, the configuration's parent configuration.
    """
    paynow: Optional[Paynow]
    paypal: Optional[Paypal]
    promptpay: Optional[Promptpay]
    revolut_pay: Optional[RevolutPay]
    sepa_debit: Optional[SepaDebit]
    sofort: Optional[Sofort]
    swish: Optional[Swish]
    twint: Optional[Twint]
    us_bank_account: Optional[UsBankAccount]
    wechat_pay: Optional[WechatPay]
    zip: Optional[Zip]

    @classmethod
    def create(
        cls, **params: Unpack["PaymentMethodConfiguration.CreateParams"]
    ) -> "PaymentMethodConfiguration":
        """
        Creates a payment method configuration
        """
        return cast(
            "PaymentMethodConfiguration",
            cls._static_request(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    async def create_async(
        cls, **params: Unpack["PaymentMethodConfiguration.CreateParams"]
    ) -> "PaymentMethodConfiguration":
        """
        Creates a payment method configuration
        """
        return cast(
            "PaymentMethodConfiguration",
            await cls._static_request_async(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    def list(
        cls, **params: Unpack["PaymentMethodConfiguration.ListParams"]
    ) -> ListObject["PaymentMethodConfiguration"]:
        """
        List payment method configurations
        """
        result = cls._static_request(
            "get",
            cls.class_url(),
            params=params,
        )
        if not isinstance(result, ListObject):
            raise TypeError(
                "Expected list object from API, got %s"
                % (type(result).__name__)
            )

        return result

    @classmethod
    async def list_async(
        cls, **params: Unpack["PaymentMethodConfiguration.ListParams"]
    ) -> ListObject["PaymentMethodConfiguration"]:
        """
        List payment method configurations
        """
        result = await cls._static_request_async(
            "get",
            cls.class_url(),
            params=params,
        )
        if not isinstance(result, ListObject):
            raise TypeError(
                "Expected list object from API, got %s"
                % (type(result).__name__)
            )

        return result

    @classmethod
    def modify(
        cls,
        id: str,
        **params: Unpack["PaymentMethodConfiguration.ModifyParams"],
    ) -> "PaymentMethodConfiguration":
        """
        Update payment method configuration
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(id))
        return cast(
            "PaymentMethodConfiguration",
            cls._static_request(
                "post",
                url,
                params=params,
            ),
        )

    @classmethod
    async def modify_async(
        cls,
        id: str,
        **params: Unpack["PaymentMethodConfiguration.ModifyParams"],
    ) -> "PaymentMethodConfiguration":
        """
        Update payment method configuration
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(id))
        return cast(
            "PaymentMethodConfiguration",
            await cls._static_request_async(
                "post",
                url,
                params=params,
            ),
        )

    @classmethod
    def retrieve(
        cls,
        id: str,
        **params: Unpack["PaymentMethodConfiguration.RetrieveParams"],
    ) -> "PaymentMethodConfiguration":
        """
        Retrieve payment method configuration
        """
        instance = cls(id, **params)
        instance.refresh()
        return instance

    @classmethod
    async def retrieve_async(
        cls,
        id: str,
        **params: Unpack["PaymentMethodConfiguration.RetrieveParams"],
    ) -> "PaymentMethodConfiguration":
        """
        Retrieve payment method configuration
        """
        instance = cls(id, **params)
        await instance.refresh_async()
        return instance

    _inner_class_types = {
        "acss_debit": AcssDebit,
        "affirm": Affirm,
        "afterpay_clearpay": AfterpayClearpay,
        "alipay": Alipay,
        "amazon_pay": AmazonPay,
        "apple_pay": ApplePay,
        "au_becs_debit": AuBecsDebit,
        "bacs_debit": BacsDebit,
        "bancontact": Bancontact,
        "blik": Blik,
        "boleto": Boleto,
        "card": Card,
        "cartes_bancaires": CartesBancaires,
        "cashapp": Cashapp,
        "customer_balance": CustomerBalance,
        "eps": Eps,
        "fpx": Fpx,
        "giropay": Giropay,
        "google_pay": GooglePay,
        "grabpay": Grabpay,
        "ideal": Ideal,
        "jcb": Jcb,
        "klarna": Klarna,
        "konbini": Konbini,
        "link": Link,
        "mobilepay": Mobilepay,
        "multibanco": Multibanco,
        "oxxo": Oxxo,
        "p24": P24,
        "paynow": Paynow,
        "paypal": Paypal,
        "promptpay": Promptpay,
        "revolut_pay": RevolutPay,
        "sepa_debit": SepaDebit,
        "sofort": Sofort,
        "swish": Swish,
        "twint": Twint,
        "us_bank_account": UsBankAccount,
        "wechat_pay": WechatPay,
        "zip": Zip,
    }

# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._createable_api_resource import CreateableAPIResource
from stripe._expandable_field import ExpandableField
from stripe._list_object import ListObject
from stripe._listable_api_resource import ListableAPIResource
from stripe._request_options import RequestOptions
from stripe._stripe_object import StripeObject
from stripe._updateable_api_resource import UpdateableAPIResource
from stripe._util import sanitize_id
from typing import ClassVar, Dict, List, Optional, Union, cast
from typing_extensions import (
    Literal,
    NotRequired,
    TypedDict,
    Unpack,
    TYPE_CHECKING,
)

if TYPE_CHECKING:
    from stripe._application import Application


class Configuration(
    CreateableAPIResource["Configuration"],
    ListableAPIResource["Configuration"],
    UpdateableAPIResource["Configuration"],
):
    """
    A portal configuration describes the functionality and behavior of a portal session.
    """

    OBJECT_NAME: ClassVar[Literal["billing_portal.configuration"]] = (
        "billing_portal.configuration"
    )

    class BusinessProfile(StripeObject):
        headline: Optional[str]
        """
        The messaging shown to customers in the portal.
        """
        privacy_policy_url: Optional[str]
        """
        A link to the business's publicly available privacy policy.
        """
        terms_of_service_url: Optional[str]
        """
        A link to the business's publicly available terms of service.
        """

    class Features(StripeObject):
        class CustomerUpdate(StripeObject):
            allowed_updates: List[
                Literal[
                    "address", "email", "name", "phone", "shipping", "tax_id"
                ]
            ]
            """
            The types of customer updates that are supported. When empty, customers are not updateable.
            """
            enabled: bool
            """
            Whether the feature is enabled.
            """

        class InvoiceHistory(StripeObject):
            enabled: bool
            """
            Whether the feature is enabled.
            """

        class PaymentMethodUpdate(StripeObject):
            enabled: bool
            """
            Whether the feature is enabled.
            """

        class SubscriptionCancel(StripeObject):
            class CancellationReason(StripeObject):
                enabled: bool
                """
                Whether the feature is enabled.
                """
                options: List[
                    Literal[
                        "customer_service",
                        "low_quality",
                        "missing_features",
                        "other",
                        "switched_service",
                        "too_complex",
                        "too_expensive",
                        "unused",
                    ]
                ]
                """
                Which cancellation reasons will be given as options to the customer.
                """

            cancellation_reason: CancellationReason
            enabled: bool
            """
            Whether the feature is enabled.
            """
            mode: Literal["at_period_end", "immediately"]
            """
            Whether to cancel subscriptions immediately or at the end of the billing period.
            """
            proration_behavior: Literal[
                "always_invoice", "create_prorations", "none"
            ]
            """
            Whether to create prorations when canceling subscriptions. Possible values are `none` and `create_prorations`.
            """
            _inner_class_types = {"cancellation_reason": CancellationReason}

        class SubscriptionUpdate(StripeObject):
            class Product(StripeObject):
                prices: List[str]
                """
                The list of price IDs which, when subscribed to, a subscription can be updated.
                """
                product: str
                """
                The product ID.
                """

            default_allowed_updates: List[
                Literal["price", "promotion_code", "quantity"]
            ]
            """
            The types of subscription updates that are supported for items listed in the `products` attribute. When empty, subscriptions are not updateable.
            """
            enabled: bool
            """
            Whether the feature is enabled.
            """
            products: Optional[List[Product]]
            """
            The list of up to 10 products that support subscription updates.
            """
            proration_behavior: Literal[
                "always_invoice", "create_prorations", "none"
            ]
            """
            Determines how to handle prorations resulting from subscription updates. Valid values are `none`, `create_prorations`, and `always_invoice`. Defaults to a value of `none` if you don't set it during creation.
            """
            _inner_class_types = {"products": Product}

        customer_update: CustomerUpdate
        invoice_history: InvoiceHistory
        payment_method_update: PaymentMethodUpdate
        subscription_cancel: SubscriptionCancel
        subscription_update: SubscriptionUpdate
        _inner_class_types = {
            "customer_update": CustomerUpdate,
            "invoice_history": InvoiceHistory,
            "payment_method_update": PaymentMethodUpdate,
            "subscription_cancel": SubscriptionCancel,
            "subscription_update": SubscriptionUpdate,
        }

    class LoginPage(StripeObject):
        enabled: bool
        """
        If `true`, a shareable `url` will be generated that will take your customers to a hosted login page for the customer portal.

        If `false`, the previously generated `url`, if any, will be deactivated.
        """
        url: Optional[str]
        """
        A shareable URL to the hosted portal login page. Your customers will be able to log in with their [email](https://stripe.com/docs/api/customers/object#customer_object-email) and receive a link to their customer portal.
        """

    class CreateParams(RequestOptions):
        business_profile: "Configuration.CreateParamsBusinessProfile"
        """
        The business information shown to customers in the portal.
        """
        default_return_url: NotRequired["Literal['']|str"]
        """
        The default URL to redirect customers to when they click on the portal's link to return to your website. This can be [overriden](https://stripe.com/docs/api/customer_portal/sessions/create#create_portal_session-return_url) when creating the session.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        features: "Configuration.CreateParamsFeatures"
        """
        Information about the features available in the portal.
        """
        login_page: NotRequired["Configuration.CreateParamsLoginPage"]
        """
        The hosted login page for this configuration. Learn more about the portal login page in our [integration docs](https://stripe.com/docs/billing/subscriptions/integrating-customer-portal#share).
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """

    class CreateParamsBusinessProfile(TypedDict):
        headline: NotRequired["Literal['']|str"]
        """
        The messaging shown to customers in the portal.
        """
        privacy_policy_url: NotRequired[str]
        """
        A link to the business's publicly available privacy policy.
        """
        terms_of_service_url: NotRequired[str]
        """
        A link to the business's publicly available terms of service.
        """

    class CreateParamsFeatures(TypedDict):
        customer_update: NotRequired[
            "Configuration.CreateParamsFeaturesCustomerUpdate"
        ]
        """
        Information about updating the customer details in the portal.
        """
        invoice_history: NotRequired[
            "Configuration.CreateParamsFeaturesInvoiceHistory"
        ]
        """
        Information about showing the billing history in the portal.
        """
        payment_method_update: NotRequired[
            "Configuration.CreateParamsFeaturesPaymentMethodUpdate"
        ]
        """
        Information about updating payment methods in the portal.
        """
        subscription_cancel: NotRequired[
            "Configuration.CreateParamsFeaturesSubscriptionCancel"
        ]
        """
        Information about canceling subscriptions in the portal.
        """
        subscription_update: NotRequired[
            "Configuration.CreateParamsFeaturesSubscriptionUpdate"
        ]
        """
        Information about updating subscriptions in the portal.
        """

    class CreateParamsFeaturesCustomerUpdate(TypedDict):
        allowed_updates: NotRequired[
            "Literal['']|List[Literal['address', 'email', 'name', 'phone', 'shipping', 'tax_id']]"
        ]
        """
        The types of customer updates that are supported. When empty, customers are not updateable.
        """
        enabled: bool
        """
        Whether the feature is enabled.
        """

    class CreateParamsFeaturesInvoiceHistory(TypedDict):
        enabled: bool
        """
        Whether the feature is enabled.
        """

    class CreateParamsFeaturesPaymentMethodUpdate(TypedDict):
        enabled: bool
        """
        Whether the feature is enabled.
        """

    class CreateParamsFeaturesSubscriptionCancel(TypedDict):
        cancellation_reason: NotRequired[
            "Configuration.CreateParamsFeaturesSubscriptionCancelCancellationReason"
        ]
        """
        Whether the cancellation reasons will be collected in the portal and which options are exposed to the customer
        """
        enabled: bool
        """
        Whether the feature is enabled.
        """
        mode: NotRequired[Literal["at_period_end", "immediately"]]
        """
        Whether to cancel subscriptions immediately or at the end of the billing period.
        """
        proration_behavior: NotRequired[
            Literal["always_invoice", "create_prorations", "none"]
        ]
        """
        Whether to create prorations when canceling subscriptions. Possible values are `none` and `create_prorations`, which is only compatible with `mode=immediately`. No prorations are generated when canceling a subscription at the end of its natural billing period.
        """

    class CreateParamsFeaturesSubscriptionCancelCancellationReason(TypedDict):
        enabled: bool
        """
        Whether the feature is enabled.
        """
        options: Union[
            Literal[""],
            List[
                Literal[
                    "customer_service",
                    "low_quality",
                    "missing_features",
                    "other",
                    "switched_service",
                    "too_complex",
                    "too_expensive",
                    "unused",
                ]
            ],
        ]
        """
        Which cancellation reasons will be given as options to the customer.
        """

    class CreateParamsFeaturesSubscriptionUpdate(TypedDict):
        default_allowed_updates: Union[
            Literal[""], List[Literal["price", "promotion_code", "quantity"]]
        ]
        """
        The types of subscription updates that are supported. When empty, subscriptions are not updateable.
        """
        enabled: bool
        """
        Whether the feature is enabled.
        """
        products: Union[
            Literal[""],
            List[
                "Configuration.CreateParamsFeaturesSubscriptionUpdateProduct"
            ],
        ]
        """
        The list of up to 10 products that support subscription updates.
        """
        proration_behavior: NotRequired[
            Literal["always_invoice", "create_prorations", "none"]
        ]
        """
        Determines how to handle prorations resulting from subscription updates. Valid values are `none`, `create_prorations`, and `always_invoice`.
        """

    class CreateParamsFeaturesSubscriptionUpdateProduct(TypedDict):
        prices: List[str]
        """
        The list of price IDs for the product that a subscription can be updated to.
        """
        product: str
        """
        The product id.
        """

    class CreateParamsLoginPage(TypedDict):
        enabled: bool
        """
        Set to `true` to generate a shareable URL [`login_page.url`](https://stripe.com/docs/api/customer_portal/configuration#portal_configuration_object-login_page-url) that will take your customers to a hosted login page for the customer portal.
        """

    class ListParams(RequestOptions):
        active: NotRequired[bool]
        """
        Only return configurations that are active or inactive (e.g., pass `true` to only list active configurations).
        """
        ending_before: NotRequired[str]
        """
        A cursor for use in pagination. `ending_before` is an object ID that defines your place in the list. For instance, if you make a list request and receive 100 objects, starting with `obj_bar`, your subsequent call can include `ending_before=obj_bar` in order to fetch the previous page of the list.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        is_default: NotRequired[bool]
        """
        Only return the default or non-default configurations (e.g., pass `true` to only list the default configuration).
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
        active: NotRequired[bool]
        """
        Whether the configuration is active and can be used to create portal sessions.
        """
        business_profile: NotRequired[
            "Configuration.ModifyParamsBusinessProfile"
        ]
        """
        The business information shown to customers in the portal.
        """
        default_return_url: NotRequired["Literal['']|str"]
        """
        The default URL to redirect customers to when they click on the portal's link to return to your website. This can be [overriden](https://stripe.com/docs/api/customer_portal/sessions/create#create_portal_session-return_url) when creating the session.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        features: NotRequired["Configuration.ModifyParamsFeatures"]
        """
        Information about the features available in the portal.
        """
        login_page: NotRequired["Configuration.ModifyParamsLoginPage"]
        """
        The hosted login page for this configuration. Learn more about the portal login page in our [integration docs](https://stripe.com/docs/billing/subscriptions/integrating-customer-portal#share).
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """

    class ModifyParamsBusinessProfile(TypedDict):
        headline: NotRequired["Literal['']|str"]
        """
        The messaging shown to customers in the portal.
        """
        privacy_policy_url: NotRequired["Literal['']|str"]
        """
        A link to the business's publicly available privacy policy.
        """
        terms_of_service_url: NotRequired["Literal['']|str"]
        """
        A link to the business's publicly available terms of service.
        """

    class ModifyParamsFeatures(TypedDict):
        customer_update: NotRequired[
            "Configuration.ModifyParamsFeaturesCustomerUpdate"
        ]
        """
        Information about updating the customer details in the portal.
        """
        invoice_history: NotRequired[
            "Configuration.ModifyParamsFeaturesInvoiceHistory"
        ]
        """
        Information about showing the billing history in the portal.
        """
        payment_method_update: NotRequired[
            "Configuration.ModifyParamsFeaturesPaymentMethodUpdate"
        ]
        """
        Information about updating payment methods in the portal.
        """
        subscription_cancel: NotRequired[
            "Configuration.ModifyParamsFeaturesSubscriptionCancel"
        ]
        """
        Information about canceling subscriptions in the portal.
        """
        subscription_update: NotRequired[
            "Configuration.ModifyParamsFeaturesSubscriptionUpdate"
        ]
        """
        Information about updating subscriptions in the portal.
        """

    class ModifyParamsFeaturesCustomerUpdate(TypedDict):
        allowed_updates: NotRequired[
            "Literal['']|List[Literal['address', 'email', 'name', 'phone', 'shipping', 'tax_id']]"
        ]
        """
        The types of customer updates that are supported. When empty, customers are not updateable.
        """
        enabled: NotRequired[bool]
        """
        Whether the feature is enabled.
        """

    class ModifyParamsFeaturesInvoiceHistory(TypedDict):
        enabled: bool
        """
        Whether the feature is enabled.
        """

    class ModifyParamsFeaturesPaymentMethodUpdate(TypedDict):
        enabled: bool
        """
        Whether the feature is enabled.
        """

    class ModifyParamsFeaturesSubscriptionCancel(TypedDict):
        cancellation_reason: NotRequired[
            "Configuration.ModifyParamsFeaturesSubscriptionCancelCancellationReason"
        ]
        """
        Whether the cancellation reasons will be collected in the portal and which options are exposed to the customer
        """
        enabled: NotRequired[bool]
        """
        Whether the feature is enabled.
        """
        mode: NotRequired[Literal["at_period_end", "immediately"]]
        """
        Whether to cancel subscriptions immediately or at the end of the billing period.
        """
        proration_behavior: NotRequired[
            Literal["always_invoice", "create_prorations", "none"]
        ]
        """
        Whether to create prorations when canceling subscriptions. Possible values are `none` and `create_prorations`, which is only compatible with `mode=immediately`. No prorations are generated when canceling a subscription at the end of its natural billing period.
        """

    class ModifyParamsFeaturesSubscriptionCancelCancellationReason(TypedDict):
        enabled: bool
        """
        Whether the feature is enabled.
        """
        options: NotRequired[
            "Literal['']|List[Literal['customer_service', 'low_quality', 'missing_features', 'other', 'switched_service', 'too_complex', 'too_expensive', 'unused']]"
        ]
        """
        Which cancellation reasons will be given as options to the customer.
        """

    class ModifyParamsFeaturesSubscriptionUpdate(TypedDict):
        default_allowed_updates: NotRequired[
            "Literal['']|List[Literal['price', 'promotion_code', 'quantity']]"
        ]
        """
        The types of subscription updates that are supported. When empty, subscriptions are not updateable.
        """
        enabled: NotRequired[bool]
        """
        Whether the feature is enabled.
        """
        products: NotRequired[
            "Literal['']|List[Configuration.ModifyParamsFeaturesSubscriptionUpdateProduct]"
        ]
        """
        The list of up to 10 products that support subscription updates.
        """
        proration_behavior: NotRequired[
            Literal["always_invoice", "create_prorations", "none"]
        ]
        """
        Determines how to handle prorations resulting from subscription updates. Valid values are `none`, `create_prorations`, and `always_invoice`.
        """

    class ModifyParamsFeaturesSubscriptionUpdateProduct(TypedDict):
        prices: List[str]
        """
        The list of price IDs for the product that a subscription can be updated to.
        """
        product: str
        """
        The product id.
        """

    class ModifyParamsLoginPage(TypedDict):
        enabled: bool
        """
        Set to `true` to generate a shareable URL [`login_page.url`](https://stripe.com/docs/api/customer_portal/configuration#portal_configuration_object-login_page-url) that will take your customers to a hosted login page for the customer portal.

        Set to `false` to deactivate the `login_page.url`.
        """

    class RetrieveParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    active: bool
    """
    Whether the configuration is active and can be used to create portal sessions.
    """
    application: Optional[ExpandableField["Application"]]
    """
    ID of the Connect Application that created the configuration.
    """
    business_profile: BusinessProfile
    created: int
    """
    Time at which the object was created. Measured in seconds since the Unix epoch.
    """
    default_return_url: Optional[str]
    """
    The default URL to redirect customers to when they click on the portal's link to return to your website. This can be [overriden](https://stripe.com/docs/api/customer_portal/sessions/create#create_portal_session-return_url) when creating the session.
    """
    features: Features
    id: str
    """
    Unique identifier for the object.
    """
    is_default: bool
    """
    Whether the configuration is the default. If `true`, this configuration can be managed in the Dashboard and portal sessions will use this configuration unless it is overriden when creating the session.
    """
    livemode: bool
    """
    Has the value `true` if the object exists in live mode or the value `false` if the object exists in test mode.
    """
    login_page: LoginPage
    metadata: Optional[Dict[str, str]]
    """
    Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format.
    """
    object: Literal["billing_portal.configuration"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """
    updated: int
    """
    Time at which the object was last updated. Measured in seconds since the Unix epoch.
    """

    @classmethod
    def create(
        cls, **params: Unpack["Configuration.CreateParams"]
    ) -> "Configuration":
        """
        Creates a configuration that describes the functionality and behavior of a PortalSession
        """
        return cast(
            "Configuration",
            cls._static_request(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    async def create_async(
        cls, **params: Unpack["Configuration.CreateParams"]
    ) -> "Configuration":
        """
        Creates a configuration that describes the functionality and behavior of a PortalSession
        """
        return cast(
            "Configuration",
            await cls._static_request_async(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    def list(
        cls, **params: Unpack["Configuration.ListParams"]
    ) -> ListObject["Configuration"]:
        """
        Returns a list of configurations that describe the functionality of the customer portal.
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
        cls, **params: Unpack["Configuration.ListParams"]
    ) -> ListObject["Configuration"]:
        """
        Returns a list of configurations that describe the functionality of the customer portal.
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
        cls, id: str, **params: Unpack["Configuration.ModifyParams"]
    ) -> "Configuration":
        """
        Updates a configuration that describes the functionality of the customer portal.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(id))
        return cast(
            "Configuration",
            cls._static_request(
                "post",
                url,
                params=params,
            ),
        )

    @classmethod
    async def modify_async(
        cls, id: str, **params: Unpack["Configuration.ModifyParams"]
    ) -> "Configuration":
        """
        Updates a configuration that describes the functionality of the customer portal.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(id))
        return cast(
            "Configuration",
            await cls._static_request_async(
                "post",
                url,
                params=params,
            ),
        )

    @classmethod
    def retrieve(
        cls, id: str, **params: Unpack["Configuration.RetrieveParams"]
    ) -> "Configuration":
        """
        Retrieves a configuration that describes the functionality of the customer portal.
        """
        instance = cls(id, **params)
        instance.refresh()
        return instance

    @classmethod
    async def retrieve_async(
        cls, id: str, **params: Unpack["Configuration.RetrieveParams"]
    ) -> "Configuration":
        """
        Retrieves a configuration that describes the functionality of the customer portal.
        """
        instance = cls(id, **params)
        await instance.refresh_async()
        return instance

    _inner_class_types = {
        "business_profile": BusinessProfile,
        "features": Features,
        "login_page": LoginPage,
    }

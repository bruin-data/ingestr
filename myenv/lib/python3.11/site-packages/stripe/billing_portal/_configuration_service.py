# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from stripe.billing_portal._configuration import Configuration
from typing import Dict, List, Union, cast
from typing_extensions import Literal, NotRequired, TypedDict


class ConfigurationService(StripeService):
    class CreateParams(TypedDict):
        business_profile: "ConfigurationService.CreateParamsBusinessProfile"
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
        features: "ConfigurationService.CreateParamsFeatures"
        """
        Information about the features available in the portal.
        """
        login_page: NotRequired["ConfigurationService.CreateParamsLoginPage"]
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
            "ConfigurationService.CreateParamsFeaturesCustomerUpdate"
        ]
        """
        Information about updating the customer details in the portal.
        """
        invoice_history: NotRequired[
            "ConfigurationService.CreateParamsFeaturesInvoiceHistory"
        ]
        """
        Information about showing the billing history in the portal.
        """
        payment_method_update: NotRequired[
            "ConfigurationService.CreateParamsFeaturesPaymentMethodUpdate"
        ]
        """
        Information about updating payment methods in the portal.
        """
        subscription_cancel: NotRequired[
            "ConfigurationService.CreateParamsFeaturesSubscriptionCancel"
        ]
        """
        Information about canceling subscriptions in the portal.
        """
        subscription_update: NotRequired[
            "ConfigurationService.CreateParamsFeaturesSubscriptionUpdate"
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
            "ConfigurationService.CreateParamsFeaturesSubscriptionCancelCancellationReason"
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
                "ConfigurationService.CreateParamsFeaturesSubscriptionUpdateProduct"
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

    class ListParams(TypedDict):
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

    class RetrieveParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class UpdateParams(TypedDict):
        active: NotRequired[bool]
        """
        Whether the configuration is active and can be used to create portal sessions.
        """
        business_profile: NotRequired[
            "ConfigurationService.UpdateParamsBusinessProfile"
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
        features: NotRequired["ConfigurationService.UpdateParamsFeatures"]
        """
        Information about the features available in the portal.
        """
        login_page: NotRequired["ConfigurationService.UpdateParamsLoginPage"]
        """
        The hosted login page for this configuration. Learn more about the portal login page in our [integration docs](https://stripe.com/docs/billing/subscriptions/integrating-customer-portal#share).
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """

    class UpdateParamsBusinessProfile(TypedDict):
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

    class UpdateParamsFeatures(TypedDict):
        customer_update: NotRequired[
            "ConfigurationService.UpdateParamsFeaturesCustomerUpdate"
        ]
        """
        Information about updating the customer details in the portal.
        """
        invoice_history: NotRequired[
            "ConfigurationService.UpdateParamsFeaturesInvoiceHistory"
        ]
        """
        Information about showing the billing history in the portal.
        """
        payment_method_update: NotRequired[
            "ConfigurationService.UpdateParamsFeaturesPaymentMethodUpdate"
        ]
        """
        Information about updating payment methods in the portal.
        """
        subscription_cancel: NotRequired[
            "ConfigurationService.UpdateParamsFeaturesSubscriptionCancel"
        ]
        """
        Information about canceling subscriptions in the portal.
        """
        subscription_update: NotRequired[
            "ConfigurationService.UpdateParamsFeaturesSubscriptionUpdate"
        ]
        """
        Information about updating subscriptions in the portal.
        """

    class UpdateParamsFeaturesCustomerUpdate(TypedDict):
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

    class UpdateParamsFeaturesInvoiceHistory(TypedDict):
        enabled: bool
        """
        Whether the feature is enabled.
        """

    class UpdateParamsFeaturesPaymentMethodUpdate(TypedDict):
        enabled: bool
        """
        Whether the feature is enabled.
        """

    class UpdateParamsFeaturesSubscriptionCancel(TypedDict):
        cancellation_reason: NotRequired[
            "ConfigurationService.UpdateParamsFeaturesSubscriptionCancelCancellationReason"
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

    class UpdateParamsFeaturesSubscriptionCancelCancellationReason(TypedDict):
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

    class UpdateParamsFeaturesSubscriptionUpdate(TypedDict):
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
            "Literal['']|List[ConfigurationService.UpdateParamsFeaturesSubscriptionUpdateProduct]"
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

    class UpdateParamsFeaturesSubscriptionUpdateProduct(TypedDict):
        prices: List[str]
        """
        The list of price IDs for the product that a subscription can be updated to.
        """
        product: str
        """
        The product id.
        """

    class UpdateParamsLoginPage(TypedDict):
        enabled: bool
        """
        Set to `true` to generate a shareable URL [`login_page.url`](https://stripe.com/docs/api/customer_portal/configuration#portal_configuration_object-login_page-url) that will take your customers to a hosted login page for the customer portal.

        Set to `false` to deactivate the `login_page.url`.
        """

    def list(
        self,
        params: "ConfigurationService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Configuration]:
        """
        Returns a list of configurations that describe the functionality of the customer portal.
        """
        return cast(
            ListObject[Configuration],
            self._request(
                "get",
                "/v1/billing_portal/configurations",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "ConfigurationService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Configuration]:
        """
        Returns a list of configurations that describe the functionality of the customer portal.
        """
        return cast(
            ListObject[Configuration],
            await self._request_async(
                "get",
                "/v1/billing_portal/configurations",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self,
        params: "ConfigurationService.CreateParams",
        options: RequestOptions = {},
    ) -> Configuration:
        """
        Creates a configuration that describes the functionality and behavior of a PortalSession
        """
        return cast(
            Configuration,
            self._request(
                "post",
                "/v1/billing_portal/configurations",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        params: "ConfigurationService.CreateParams",
        options: RequestOptions = {},
    ) -> Configuration:
        """
        Creates a configuration that describes the functionality and behavior of a PortalSession
        """
        return cast(
            Configuration,
            await self._request_async(
                "post",
                "/v1/billing_portal/configurations",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        configuration: str,
        params: "ConfigurationService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Configuration:
        """
        Retrieves a configuration that describes the functionality of the customer portal.
        """
        return cast(
            Configuration,
            self._request(
                "get",
                "/v1/billing_portal/configurations/{configuration}".format(
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
        params: "ConfigurationService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Configuration:
        """
        Retrieves a configuration that describes the functionality of the customer portal.
        """
        return cast(
            Configuration,
            await self._request_async(
                "get",
                "/v1/billing_portal/configurations/{configuration}".format(
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
        params: "ConfigurationService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Configuration:
        """
        Updates a configuration that describes the functionality of the customer portal.
        """
        return cast(
            Configuration,
            self._request(
                "post",
                "/v1/billing_portal/configurations/{configuration}".format(
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
        params: "ConfigurationService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Configuration:
        """
        Updates a configuration that describes the functionality of the customer portal.
        """
        return cast(
            Configuration,
            await self._request_async(
                "post",
                "/v1/billing_portal/configurations/{configuration}".format(
                    configuration=sanitize_id(configuration),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

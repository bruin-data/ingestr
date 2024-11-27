# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._payment_method_domain import PaymentMethodDomain
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from typing import List, cast
from typing_extensions import NotRequired, TypedDict


class PaymentMethodDomainService(StripeService):
    class CreateParams(TypedDict):
        domain_name: str
        """
        The domain name that this payment method domain object represents.
        """
        enabled: NotRequired[bool]
        """
        Whether this payment method domain is enabled. If the domain is not enabled, payment methods that require a payment method domain will not appear in Elements.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class ListParams(TypedDict):
        domain_name: NotRequired[str]
        """
        The domain name that this payment method domain object represents.
        """
        enabled: NotRequired[bool]
        """
        Whether this payment method domain is enabled. If the domain is not enabled, payment methods will not appear in Elements
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
        enabled: NotRequired[bool]
        """
        Whether this payment method domain is enabled. If the domain is not enabled, payment methods that require a payment method domain will not appear in Elements.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class ValidateParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    def list(
        self,
        params: "PaymentMethodDomainService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[PaymentMethodDomain]:
        """
        Lists the details of existing payment method domains.
        """
        return cast(
            ListObject[PaymentMethodDomain],
            self._request(
                "get",
                "/v1/payment_method_domains",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "PaymentMethodDomainService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[PaymentMethodDomain]:
        """
        Lists the details of existing payment method domains.
        """
        return cast(
            ListObject[PaymentMethodDomain],
            await self._request_async(
                "get",
                "/v1/payment_method_domains",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self,
        params: "PaymentMethodDomainService.CreateParams",
        options: RequestOptions = {},
    ) -> PaymentMethodDomain:
        """
        Creates a payment method domain.
        """
        return cast(
            PaymentMethodDomain,
            self._request(
                "post",
                "/v1/payment_method_domains",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        params: "PaymentMethodDomainService.CreateParams",
        options: RequestOptions = {},
    ) -> PaymentMethodDomain:
        """
        Creates a payment method domain.
        """
        return cast(
            PaymentMethodDomain,
            await self._request_async(
                "post",
                "/v1/payment_method_domains",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        payment_method_domain: str,
        params: "PaymentMethodDomainService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> PaymentMethodDomain:
        """
        Retrieves the details of an existing payment method domain.
        """
        return cast(
            PaymentMethodDomain,
            self._request(
                "get",
                "/v1/payment_method_domains/{payment_method_domain}".format(
                    payment_method_domain=sanitize_id(payment_method_domain),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        payment_method_domain: str,
        params: "PaymentMethodDomainService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> PaymentMethodDomain:
        """
        Retrieves the details of an existing payment method domain.
        """
        return cast(
            PaymentMethodDomain,
            await self._request_async(
                "get",
                "/v1/payment_method_domains/{payment_method_domain}".format(
                    payment_method_domain=sanitize_id(payment_method_domain),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def update(
        self,
        payment_method_domain: str,
        params: "PaymentMethodDomainService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> PaymentMethodDomain:
        """
        Updates an existing payment method domain.
        """
        return cast(
            PaymentMethodDomain,
            self._request(
                "post",
                "/v1/payment_method_domains/{payment_method_domain}".format(
                    payment_method_domain=sanitize_id(payment_method_domain),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def update_async(
        self,
        payment_method_domain: str,
        params: "PaymentMethodDomainService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> PaymentMethodDomain:
        """
        Updates an existing payment method domain.
        """
        return cast(
            PaymentMethodDomain,
            await self._request_async(
                "post",
                "/v1/payment_method_domains/{payment_method_domain}".format(
                    payment_method_domain=sanitize_id(payment_method_domain),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def validate(
        self,
        payment_method_domain: str,
        params: "PaymentMethodDomainService.ValidateParams" = {},
        options: RequestOptions = {},
    ) -> PaymentMethodDomain:
        """
        Some payment methods such as Apple Pay require additional steps to verify a domain. If the requirements weren't satisfied when the domain was created, the payment method will be inactive on the domain.
        The payment method doesn't appear in Elements for this domain until it is active.

        To activate a payment method on an existing payment method domain, complete the required validation steps specific to the payment method, and then validate the payment method domain with this endpoint.

        Related guides: [Payment method domains](https://stripe.com/docs/payments/payment-methods/pmd-registration).
        """
        return cast(
            PaymentMethodDomain,
            self._request(
                "post",
                "/v1/payment_method_domains/{payment_method_domain}/validate".format(
                    payment_method_domain=sanitize_id(payment_method_domain),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def validate_async(
        self,
        payment_method_domain: str,
        params: "PaymentMethodDomainService.ValidateParams" = {},
        options: RequestOptions = {},
    ) -> PaymentMethodDomain:
        """
        Some payment methods such as Apple Pay require additional steps to verify a domain. If the requirements weren't satisfied when the domain was created, the payment method will be inactive on the domain.
        The payment method doesn't appear in Elements for this domain until it is active.

        To activate a payment method on an existing payment method domain, complete the required validation steps specific to the payment method, and then validate the payment method domain with this endpoint.

        Related guides: [Payment method domains](https://stripe.com/docs/payments/payment-methods/pmd-registration).
        """
        return cast(
            PaymentMethodDomain,
            await self._request_async(
                "post",
                "/v1/payment_method_domains/{payment_method_domain}/validate".format(
                    payment_method_domain=sanitize_id(payment_method_domain),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

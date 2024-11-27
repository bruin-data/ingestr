# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._apple_pay_domain import ApplePayDomain
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from typing import List, cast
from typing_extensions import NotRequired, TypedDict


class ApplePayDomainService(StripeService):
    class CreateParams(TypedDict):
        domain_name: str
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class DeleteParams(TypedDict):
        pass

    class ListParams(TypedDict):
        domain_name: NotRequired[str]
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

    def delete(
        self,
        domain: str,
        params: "ApplePayDomainService.DeleteParams" = {},
        options: RequestOptions = {},
    ) -> ApplePayDomain:
        """
        Delete an apple pay domain.
        """
        return cast(
            ApplePayDomain,
            self._request(
                "delete",
                "/v1/apple_pay/domains/{domain}".format(
                    domain=sanitize_id(domain),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def delete_async(
        self,
        domain: str,
        params: "ApplePayDomainService.DeleteParams" = {},
        options: RequestOptions = {},
    ) -> ApplePayDomain:
        """
        Delete an apple pay domain.
        """
        return cast(
            ApplePayDomain,
            await self._request_async(
                "delete",
                "/v1/apple_pay/domains/{domain}".format(
                    domain=sanitize_id(domain),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        domain: str,
        params: "ApplePayDomainService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> ApplePayDomain:
        """
        Retrieve an apple pay domain.
        """
        return cast(
            ApplePayDomain,
            self._request(
                "get",
                "/v1/apple_pay/domains/{domain}".format(
                    domain=sanitize_id(domain),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        domain: str,
        params: "ApplePayDomainService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> ApplePayDomain:
        """
        Retrieve an apple pay domain.
        """
        return cast(
            ApplePayDomain,
            await self._request_async(
                "get",
                "/v1/apple_pay/domains/{domain}".format(
                    domain=sanitize_id(domain),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def list(
        self,
        params: "ApplePayDomainService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[ApplePayDomain]:
        """
        List apple pay domains.
        """
        return cast(
            ListObject[ApplePayDomain],
            self._request(
                "get",
                "/v1/apple_pay/domains",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "ApplePayDomainService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[ApplePayDomain]:
        """
        List apple pay domains.
        """
        return cast(
            ListObject[ApplePayDomain],
            await self._request_async(
                "get",
                "/v1/apple_pay/domains",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self,
        params: "ApplePayDomainService.CreateParams",
        options: RequestOptions = {},
    ) -> ApplePayDomain:
        """
        Create an apple pay domain.
        """
        return cast(
            ApplePayDomain,
            self._request(
                "post",
                "/v1/apple_pay/domains",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        params: "ApplePayDomainService.CreateParams",
        options: RequestOptions = {},
    ) -> ApplePayDomain:
        """
        Create an apple pay domain.
        """
        return cast(
            ApplePayDomain,
            await self._request_async(
                "post",
                "/v1/apple_pay/domains",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

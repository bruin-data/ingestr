# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._product_feature import ProductFeature
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from typing import List, cast
from typing_extensions import NotRequired, TypedDict


class ProductFeatureService(StripeService):
    class CreateParams(TypedDict):
        entitlement_feature: str
        """
        The ID of the [Feature](https://stripe.com/docs/api/entitlements/feature) object attached to this product.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class DeleteParams(TypedDict):
        pass

    class ListParams(TypedDict):
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
        product: str,
        id: str,
        params: "ProductFeatureService.DeleteParams" = {},
        options: RequestOptions = {},
    ) -> ProductFeature:
        """
        Deletes the feature attachment to a product
        """
        return cast(
            ProductFeature,
            self._request(
                "delete",
                "/v1/products/{product}/features/{id}".format(
                    product=sanitize_id(product),
                    id=sanitize_id(id),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def delete_async(
        self,
        product: str,
        id: str,
        params: "ProductFeatureService.DeleteParams" = {},
        options: RequestOptions = {},
    ) -> ProductFeature:
        """
        Deletes the feature attachment to a product
        """
        return cast(
            ProductFeature,
            await self._request_async(
                "delete",
                "/v1/products/{product}/features/{id}".format(
                    product=sanitize_id(product),
                    id=sanitize_id(id),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        product: str,
        id: str,
        params: "ProductFeatureService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> ProductFeature:
        """
        Retrieves a product_feature, which represents a feature attachment to a product
        """
        return cast(
            ProductFeature,
            self._request(
                "get",
                "/v1/products/{product}/features/{id}".format(
                    product=sanitize_id(product),
                    id=sanitize_id(id),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        product: str,
        id: str,
        params: "ProductFeatureService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> ProductFeature:
        """
        Retrieves a product_feature, which represents a feature attachment to a product
        """
        return cast(
            ProductFeature,
            await self._request_async(
                "get",
                "/v1/products/{product}/features/{id}".format(
                    product=sanitize_id(product),
                    id=sanitize_id(id),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def list(
        self,
        product: str,
        params: "ProductFeatureService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[ProductFeature]:
        """
        Retrieve a list of features for a product
        """
        return cast(
            ListObject[ProductFeature],
            self._request(
                "get",
                "/v1/products/{product}/features".format(
                    product=sanitize_id(product),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        product: str,
        params: "ProductFeatureService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[ProductFeature]:
        """
        Retrieve a list of features for a product
        """
        return cast(
            ListObject[ProductFeature],
            await self._request_async(
                "get",
                "/v1/products/{product}/features".format(
                    product=sanitize_id(product),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self,
        product: str,
        params: "ProductFeatureService.CreateParams",
        options: RequestOptions = {},
    ) -> ProductFeature:
        """
        Creates a product_feature, which represents a feature attachment to a product
        """
        return cast(
            ProductFeature,
            self._request(
                "post",
                "/v1/products/{product}/features".format(
                    product=sanitize_id(product),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        product: str,
        params: "ProductFeatureService.CreateParams",
        options: RequestOptions = {},
    ) -> ProductFeature:
        """
        Creates a product_feature, which represents a feature attachment to a product
        """
        return cast(
            ProductFeature,
            await self._request_async(
                "post",
                "/v1/products/{product}/features".format(
                    product=sanitize_id(product),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

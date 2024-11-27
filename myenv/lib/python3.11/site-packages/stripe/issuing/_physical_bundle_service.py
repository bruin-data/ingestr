# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from stripe.issuing._physical_bundle import PhysicalBundle
from typing import List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class PhysicalBundleService(StripeService):
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
        status: NotRequired[Literal["active", "inactive", "review"]]
        """
        Only return physical bundles with the given status.
        """
        type: NotRequired[Literal["custom", "standard"]]
        """
        Only return physical bundles with the given type.
        """

    class RetrieveParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    def list(
        self,
        params: "PhysicalBundleService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[PhysicalBundle]:
        """
        Returns a list of physical bundle objects. The objects are sorted in descending order by creation date, with the most recently created object appearing first.
        """
        return cast(
            ListObject[PhysicalBundle],
            self._request(
                "get",
                "/v1/issuing/physical_bundles",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "PhysicalBundleService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[PhysicalBundle]:
        """
        Returns a list of physical bundle objects. The objects are sorted in descending order by creation date, with the most recently created object appearing first.
        """
        return cast(
            ListObject[PhysicalBundle],
            await self._request_async(
                "get",
                "/v1/issuing/physical_bundles",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        physical_bundle: str,
        params: "PhysicalBundleService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> PhysicalBundle:
        """
        Retrieves a physical bundle object.
        """
        return cast(
            PhysicalBundle,
            self._request(
                "get",
                "/v1/issuing/physical_bundles/{physical_bundle}".format(
                    physical_bundle=sanitize_id(physical_bundle),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        physical_bundle: str,
        params: "PhysicalBundleService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> PhysicalBundle:
        """
        Retrieves a physical bundle object.
        """
        return cast(
            PhysicalBundle,
            await self._request_async(
                "get",
                "/v1/issuing/physical_bundles/{physical_bundle}".format(
                    physical_bundle=sanitize_id(physical_bundle),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

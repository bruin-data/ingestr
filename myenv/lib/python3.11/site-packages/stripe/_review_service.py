# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._review import Review
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from typing import List, cast
from typing_extensions import NotRequired, TypedDict


class ReviewService(StripeService):
    class ApproveParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class ListParams(TypedDict):
        created: NotRequired["ReviewService.ListParamsCreated|int"]
        """
        Only return reviews that were created during the given date interval.
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

    class ListParamsCreated(TypedDict):
        gt: NotRequired[int]
        """
        Minimum value to filter by (exclusive)
        """
        gte: NotRequired[int]
        """
        Minimum value to filter by (inclusive)
        """
        lt: NotRequired[int]
        """
        Maximum value to filter by (exclusive)
        """
        lte: NotRequired[int]
        """
        Maximum value to filter by (inclusive)
        """

    class RetrieveParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    def list(
        self,
        params: "ReviewService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Review]:
        """
        Returns a list of Review objects that have open set to true. The objects are sorted in descending order by creation date, with the most recently created object appearing first.
        """
        return cast(
            ListObject[Review],
            self._request(
                "get",
                "/v1/reviews",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "ReviewService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Review]:
        """
        Returns a list of Review objects that have open set to true. The objects are sorted in descending order by creation date, with the most recently created object appearing first.
        """
        return cast(
            ListObject[Review],
            await self._request_async(
                "get",
                "/v1/reviews",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        review: str,
        params: "ReviewService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Review:
        """
        Retrieves a Review object.
        """
        return cast(
            Review,
            self._request(
                "get",
                "/v1/reviews/{review}".format(review=sanitize_id(review)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        review: str,
        params: "ReviewService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Review:
        """
        Retrieves a Review object.
        """
        return cast(
            Review,
            await self._request_async(
                "get",
                "/v1/reviews/{review}".format(review=sanitize_id(review)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def approve(
        self,
        review: str,
        params: "ReviewService.ApproveParams" = {},
        options: RequestOptions = {},
    ) -> Review:
        """
        Approves a Review object, closing it and removing it from the list of reviews.
        """
        return cast(
            Review,
            self._request(
                "post",
                "/v1/reviews/{review}/approve".format(
                    review=sanitize_id(review),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def approve_async(
        self,
        review: str,
        params: "ReviewService.ApproveParams" = {},
        options: RequestOptions = {},
    ) -> Review:
        """
        Approves a Review object, closing it and removing it from the list of reviews.
        """
        return cast(
            Review,
            await self._request_async(
                "post",
                "/v1/reviews/{review}/approve".format(
                    review=sanitize_id(review),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

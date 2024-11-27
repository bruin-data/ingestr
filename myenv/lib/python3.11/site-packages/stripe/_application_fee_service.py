# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._application_fee import ApplicationFee
from stripe._application_fee_refund_service import ApplicationFeeRefundService
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from typing import List, cast
from typing_extensions import NotRequired, TypedDict


class ApplicationFeeService(StripeService):
    def __init__(self, requestor):
        super().__init__(requestor)
        self.refunds = ApplicationFeeRefundService(self._requestor)

    class ListParams(TypedDict):
        charge: NotRequired[str]
        """
        Only return application fees for the charge specified by this charge ID.
        """
        created: NotRequired["ApplicationFeeService.ListParamsCreated|int"]
        """
        Only return applications fees that were created during the given date interval.
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
        params: "ApplicationFeeService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[ApplicationFee]:
        """
        Returns a list of application fees you've previously collected. The application fees are returned in sorted order, with the most recent fees appearing first.
        """
        return cast(
            ListObject[ApplicationFee],
            self._request(
                "get",
                "/v1/application_fees",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "ApplicationFeeService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[ApplicationFee]:
        """
        Returns a list of application fees you've previously collected. The application fees are returned in sorted order, with the most recent fees appearing first.
        """
        return cast(
            ListObject[ApplicationFee],
            await self._request_async(
                "get",
                "/v1/application_fees",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        id: str,
        params: "ApplicationFeeService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> ApplicationFee:
        """
        Retrieves the details of an application fee that your account has collected. The same information is returned when refunding the application fee.
        """
        return cast(
            ApplicationFee,
            self._request(
                "get",
                "/v1/application_fees/{id}".format(id=sanitize_id(id)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        id: str,
        params: "ApplicationFeeService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> ApplicationFee:
        """
        Retrieves the details of an application fee that your account has collected. The same information is returned when refunding the application fee.
        """
        return cast(
            ApplicationFee,
            await self._request_async(
                "get",
                "/v1/application_fees/{id}".format(id=sanitize_id(id)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from stripe.sigma._scheduled_query_run import ScheduledQueryRun
from typing import List, cast
from typing_extensions import NotRequired, TypedDict


class ScheduledQueryRunService(StripeService):
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

    def list(
        self,
        params: "ScheduledQueryRunService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[ScheduledQueryRun]:
        """
        Returns a list of scheduled query runs.
        """
        return cast(
            ListObject[ScheduledQueryRun],
            self._request(
                "get",
                "/v1/sigma/scheduled_query_runs",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "ScheduledQueryRunService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[ScheduledQueryRun]:
        """
        Returns a list of scheduled query runs.
        """
        return cast(
            ListObject[ScheduledQueryRun],
            await self._request_async(
                "get",
                "/v1/sigma/scheduled_query_runs",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        scheduled_query_run: str,
        params: "ScheduledQueryRunService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> ScheduledQueryRun:
        """
        Retrieves the details of an scheduled query run.
        """
        return cast(
            ScheduledQueryRun,
            self._request(
                "get",
                "/v1/sigma/scheduled_query_runs/{scheduled_query_run}".format(
                    scheduled_query_run=sanitize_id(scheduled_query_run),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        scheduled_query_run: str,
        params: "ScheduledQueryRunService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> ScheduledQueryRun:
        """
        Retrieves the details of an scheduled query run.
        """
        return cast(
            ScheduledQueryRun,
            await self._request_async(
                "get",
                "/v1/sigma/scheduled_query_runs/{scheduled_query_run}".format(
                    scheduled_query_run=sanitize_id(scheduled_query_run),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

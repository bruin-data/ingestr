# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from stripe.reporting._report_type import ReportType
from typing import List, cast
from typing_extensions import NotRequired, TypedDict


class ReportTypeService(StripeService):
    class ListParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class RetrieveParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    def list(
        self,
        params: "ReportTypeService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[ReportType]:
        """
        Returns a full list of Report Types.
        """
        return cast(
            ListObject[ReportType],
            self._request(
                "get",
                "/v1/reporting/report_types",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "ReportTypeService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[ReportType]:
        """
        Returns a full list of Report Types.
        """
        return cast(
            ListObject[ReportType],
            await self._request_async(
                "get",
                "/v1/reporting/report_types",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        report_type: str,
        params: "ReportTypeService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> ReportType:
        """
        Retrieves the details of a Report Type. (Certain report types require a [live-mode API key](https://stripe.com/docs/keys#test-live-modes).)
        """
        return cast(
            ReportType,
            self._request(
                "get",
                "/v1/reporting/report_types/{report_type}".format(
                    report_type=sanitize_id(report_type),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        report_type: str,
        params: "ReportTypeService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> ReportType:
        """
        Retrieves the details of a Report Type. (Certain report types require a [live-mode API key](https://stripe.com/docs/keys#test-live-modes).)
        """
        return cast(
            ReportType,
            await self._request_async(
                "get",
                "/v1/reporting/report_types/{report_type}".format(
                    report_type=sanitize_id(report_type),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

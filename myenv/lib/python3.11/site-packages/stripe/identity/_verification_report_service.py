# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from stripe.identity._verification_report import VerificationReport
from typing import List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class VerificationReportService(StripeService):
    class ListParams(TypedDict):
        client_reference_id: NotRequired[str]
        """
        A string to reference this user. This can be a customer ID, a session ID, or similar, and can be used to reconcile this verification with your internal systems.
        """
        created: NotRequired["VerificationReportService.ListParamsCreated|int"]
        """
        Only return VerificationReports that were created during the given date interval.
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
        type: NotRequired[Literal["document", "id_number"]]
        """
        Only return VerificationReports of this type
        """
        verification_session: NotRequired[str]
        """
        Only return VerificationReports created by this VerificationSession ID. It is allowed to provide a VerificationIntent ID.
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
        params: "VerificationReportService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[VerificationReport]:
        """
        List all verification reports.
        """
        return cast(
            ListObject[VerificationReport],
            self._request(
                "get",
                "/v1/identity/verification_reports",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "VerificationReportService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[VerificationReport]:
        """
        List all verification reports.
        """
        return cast(
            ListObject[VerificationReport],
            await self._request_async(
                "get",
                "/v1/identity/verification_reports",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        report: str,
        params: "VerificationReportService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> VerificationReport:
        """
        Retrieves an existing VerificationReport
        """
        return cast(
            VerificationReport,
            self._request(
                "get",
                "/v1/identity/verification_reports/{report}".format(
                    report=sanitize_id(report),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        report: str,
        params: "VerificationReportService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> VerificationReport:
        """
        Retrieves an existing VerificationReport
        """
        return cast(
            VerificationReport,
            await self._request_async(
                "get",
                "/v1/identity/verification_reports/{report}".format(
                    report=sanitize_id(report),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

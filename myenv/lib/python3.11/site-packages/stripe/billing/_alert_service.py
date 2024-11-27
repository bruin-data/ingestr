# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from stripe.billing._alert import Alert
from typing import List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class AlertService(StripeService):
    class ActivateParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class ArchiveParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class CreateParams(TypedDict):
        alert_type: Literal["usage_threshold"]
        """
        The type of alert to create.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        filter: NotRequired["AlertService.CreateParamsFilter"]
        """
        Filters to limit the scope of an alert.
        """
        title: str
        """
        The title of the alert.
        """
        usage_threshold_config: NotRequired[
            "AlertService.CreateParamsUsageThresholdConfig"
        ]
        """
        The configuration of the usage threshold.
        """

    class CreateParamsFilter(TypedDict):
        customer: NotRequired[str]
        """
        Limit the scope to this alert only to this customer.
        """

    class CreateParamsUsageThresholdConfig(TypedDict):
        gte: int
        """
        Defines at which value the alert will fire.
        """
        meter: NotRequired[str]
        """
        The [Billing Meter](https://stripe.com/api/billing/meter) ID whose usage is monitored.
        """
        recurrence: Literal["one_time"]
        """
        Whether the alert should only fire only once, or once per billing cycle.
        """

    class DeactivateParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class ListParams(TypedDict):
        alert_type: NotRequired[Literal["usage_threshold"]]
        """
        Filter results to only include this type of alert.
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
        meter: NotRequired[str]
        """
        Filter results to only include alerts with the given meter.
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
        params: "AlertService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Alert]:
        """
        Lists billing active and inactive alerts
        """
        return cast(
            ListObject[Alert],
            self._request(
                "get",
                "/v1/billing/alerts",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "AlertService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Alert]:
        """
        Lists billing active and inactive alerts
        """
        return cast(
            ListObject[Alert],
            await self._request_async(
                "get",
                "/v1/billing/alerts",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self, params: "AlertService.CreateParams", options: RequestOptions = {}
    ) -> Alert:
        """
        Creates a billing alert
        """
        return cast(
            Alert,
            self._request(
                "post",
                "/v1/billing/alerts",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self, params: "AlertService.CreateParams", options: RequestOptions = {}
    ) -> Alert:
        """
        Creates a billing alert
        """
        return cast(
            Alert,
            await self._request_async(
                "post",
                "/v1/billing/alerts",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        id: str,
        params: "AlertService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Alert:
        """
        Retrieves a billing alert given an ID
        """
        return cast(
            Alert,
            self._request(
                "get",
                "/v1/billing/alerts/{id}".format(id=sanitize_id(id)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        id: str,
        params: "AlertService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Alert:
        """
        Retrieves a billing alert given an ID
        """
        return cast(
            Alert,
            await self._request_async(
                "get",
                "/v1/billing/alerts/{id}".format(id=sanitize_id(id)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def activate(
        self,
        id: str,
        params: "AlertService.ActivateParams" = {},
        options: RequestOptions = {},
    ) -> Alert:
        """
        Reactivates this alert, allowing it to trigger again.
        """
        return cast(
            Alert,
            self._request(
                "post",
                "/v1/billing/alerts/{id}/activate".format(id=sanitize_id(id)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def activate_async(
        self,
        id: str,
        params: "AlertService.ActivateParams" = {},
        options: RequestOptions = {},
    ) -> Alert:
        """
        Reactivates this alert, allowing it to trigger again.
        """
        return cast(
            Alert,
            await self._request_async(
                "post",
                "/v1/billing/alerts/{id}/activate".format(id=sanitize_id(id)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def archive(
        self,
        id: str,
        params: "AlertService.ArchiveParams" = {},
        options: RequestOptions = {},
    ) -> Alert:
        """
        Archives this alert, removing it from the list view and APIs. This is non-reversible.
        """
        return cast(
            Alert,
            self._request(
                "post",
                "/v1/billing/alerts/{id}/archive".format(id=sanitize_id(id)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def archive_async(
        self,
        id: str,
        params: "AlertService.ArchiveParams" = {},
        options: RequestOptions = {},
    ) -> Alert:
        """
        Archives this alert, removing it from the list view and APIs. This is non-reversible.
        """
        return cast(
            Alert,
            await self._request_async(
                "post",
                "/v1/billing/alerts/{id}/archive".format(id=sanitize_id(id)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def deactivate(
        self,
        id: str,
        params: "AlertService.DeactivateParams" = {},
        options: RequestOptions = {},
    ) -> Alert:
        """
        Deactivates this alert, preventing it from triggering.
        """
        return cast(
            Alert,
            self._request(
                "post",
                "/v1/billing/alerts/{id}/deactivate".format(
                    id=sanitize_id(id)
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def deactivate_async(
        self,
        id: str,
        params: "AlertService.DeactivateParams" = {},
        options: RequestOptions = {},
    ) -> Alert:
        """
        Deactivates this alert, preventing it from triggering.
        """
        return cast(
            Alert,
            await self._request_async(
                "post",
                "/v1/billing/alerts/{id}/deactivate".format(
                    id=sanitize_id(id)
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

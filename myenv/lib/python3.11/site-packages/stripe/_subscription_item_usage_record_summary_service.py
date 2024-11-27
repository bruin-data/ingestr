# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._usage_record_summary import UsageRecordSummary
from stripe._util import sanitize_id
from typing import List, cast
from typing_extensions import NotRequired, TypedDict


class SubscriptionItemUsageRecordSummaryService(StripeService):
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

    def list(
        self,
        subscription_item: str,
        params: "SubscriptionItemUsageRecordSummaryService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[UsageRecordSummary]:
        """
        For the specified subscription item, returns a list of summary objects. Each object in the list provides usage information that's been summarized from multiple usage records and over a subscription billing period (e.g., 15 usage records in the month of September).

        The list is sorted in reverse-chronological order (newest first). The first list item represents the most current usage period that hasn't ended yet. Since new usage records can still be added, the returned summary information for the subscription item's ID should be seen as unstable until the subscription billing period ends.
        """
        return cast(
            ListObject[UsageRecordSummary],
            self._request(
                "get",
                "/v1/subscription_items/{subscription_item}/usage_record_summaries".format(
                    subscription_item=sanitize_id(subscription_item),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        subscription_item: str,
        params: "SubscriptionItemUsageRecordSummaryService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[UsageRecordSummary]:
        """
        For the specified subscription item, returns a list of summary objects. Each object in the list provides usage information that's been summarized from multiple usage records and over a subscription billing period (e.g., 15 usage records in the month of September).

        The list is sorted in reverse-chronological order (newest first). The first list item represents the most current usage period that hasn't ended yet. Since new usage records can still be added, the returned summary information for the subscription item's ID should be seen as unstable until the subscription billing period ends.
        """
        return cast(
            ListObject[UsageRecordSummary],
            await self._request_async(
                "get",
                "/v1/subscription_items/{subscription_item}/usage_record_summaries".format(
                    subscription_item=sanitize_id(subscription_item),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

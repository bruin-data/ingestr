# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from stripe.treasury._received_credit import ReceivedCredit
from typing import List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class ReceivedCreditService(StripeService):
    class ListParams(TypedDict):
        ending_before: NotRequired[str]
        """
        A cursor for use in pagination. `ending_before` is an object ID that defines your place in the list. For instance, if you make a list request and receive 100 objects, starting with `obj_bar`, your subsequent call can include `ending_before=obj_bar` in order to fetch the previous page of the list.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        financial_account: str
        """
        The FinancialAccount that received the funds.
        """
        limit: NotRequired[int]
        """
        A limit on the number of objects to be returned. Limit can range between 1 and 100, and the default is 10.
        """
        linked_flows: NotRequired[
            "ReceivedCreditService.ListParamsLinkedFlows"
        ]
        """
        Only return ReceivedCredits described by the flow.
        """
        starting_after: NotRequired[str]
        """
        A cursor for use in pagination. `starting_after` is an object ID that defines your place in the list. For instance, if you make a list request and receive 100 objects, ending with `obj_foo`, your subsequent call can include `starting_after=obj_foo` in order to fetch the next page of the list.
        """
        status: NotRequired[Literal["failed", "succeeded"]]
        """
        Only return ReceivedCredits that have the given status: `succeeded` or `failed`.
        """

    class ListParamsLinkedFlows(TypedDict):
        source_flow_type: Literal[
            "credit_reversal", "other", "outbound_payment", "payout"
        ]
        """
        The source flow type.
        """

    class RetrieveParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    def list(
        self,
        params: "ReceivedCreditService.ListParams",
        options: RequestOptions = {},
    ) -> ListObject[ReceivedCredit]:
        """
        Returns a list of ReceivedCredits.
        """
        return cast(
            ListObject[ReceivedCredit],
            self._request(
                "get",
                "/v1/treasury/received_credits",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "ReceivedCreditService.ListParams",
        options: RequestOptions = {},
    ) -> ListObject[ReceivedCredit]:
        """
        Returns a list of ReceivedCredits.
        """
        return cast(
            ListObject[ReceivedCredit],
            await self._request_async(
                "get",
                "/v1/treasury/received_credits",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        id: str,
        params: "ReceivedCreditService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> ReceivedCredit:
        """
        Retrieves the details of an existing ReceivedCredit by passing the unique ReceivedCredit ID from the ReceivedCredit list.
        """
        return cast(
            ReceivedCredit,
            self._request(
                "get",
                "/v1/treasury/received_credits/{id}".format(
                    id=sanitize_id(id)
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        id: str,
        params: "ReceivedCreditService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> ReceivedCredit:
        """
        Retrieves the details of an existing ReceivedCredit by passing the unique ReceivedCredit ID from the ReceivedCredit list.
        """
        return cast(
            ReceivedCredit,
            await self._request_async(
                "get",
                "/v1/treasury/received_credits/{id}".format(
                    id=sanitize_id(id)
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

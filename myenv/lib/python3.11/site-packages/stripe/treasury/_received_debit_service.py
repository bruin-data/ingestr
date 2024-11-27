# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from stripe.treasury._received_debit import ReceivedDebit
from typing import List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class ReceivedDebitService(StripeService):
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
        The FinancialAccount that funds were pulled from.
        """
        limit: NotRequired[int]
        """
        A limit on the number of objects to be returned. Limit can range between 1 and 100, and the default is 10.
        """
        starting_after: NotRequired[str]
        """
        A cursor for use in pagination. `starting_after` is an object ID that defines your place in the list. For instance, if you make a list request and receive 100 objects, ending with `obj_foo`, your subsequent call can include `starting_after=obj_foo` in order to fetch the next page of the list.
        """
        status: NotRequired[Literal["failed", "succeeded"]]
        """
        Only return ReceivedDebits that have the given status: `succeeded` or `failed`.
        """

    class RetrieveParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    def list(
        self,
        params: "ReceivedDebitService.ListParams",
        options: RequestOptions = {},
    ) -> ListObject[ReceivedDebit]:
        """
        Returns a list of ReceivedDebits.
        """
        return cast(
            ListObject[ReceivedDebit],
            self._request(
                "get",
                "/v1/treasury/received_debits",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "ReceivedDebitService.ListParams",
        options: RequestOptions = {},
    ) -> ListObject[ReceivedDebit]:
        """
        Returns a list of ReceivedDebits.
        """
        return cast(
            ListObject[ReceivedDebit],
            await self._request_async(
                "get",
                "/v1/treasury/received_debits",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        id: str,
        params: "ReceivedDebitService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> ReceivedDebit:
        """
        Retrieves the details of an existing ReceivedDebit by passing the unique ReceivedDebit ID from the ReceivedDebit list
        """
        return cast(
            ReceivedDebit,
            self._request(
                "get",
                "/v1/treasury/received_debits/{id}".format(id=sanitize_id(id)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        id: str,
        params: "ReceivedDebitService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> ReceivedDebit:
        """
        Retrieves the details of an existing ReceivedDebit by passing the unique ReceivedDebit ID from the ReceivedDebit list
        """
        return cast(
            ReceivedDebit,
            await self._request_async(
                "get",
                "/v1/treasury/received_debits/{id}".format(id=sanitize_id(id)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

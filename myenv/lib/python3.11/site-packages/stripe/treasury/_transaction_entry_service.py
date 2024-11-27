# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from stripe.treasury._transaction_entry import TransactionEntry
from typing import List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class TransactionEntryService(StripeService):
    class ListParams(TypedDict):
        created: NotRequired["TransactionEntryService.ListParamsCreated|int"]
        """
        Only return TransactionEntries that were created during the given date interval.
        """
        effective_at: NotRequired[
            "TransactionEntryService.ListParamsEffectiveAt|int"
        ]
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
        Returns objects associated with this FinancialAccount.
        """
        limit: NotRequired[int]
        """
        A limit on the number of objects to be returned. Limit can range between 1 and 100, and the default is 10.
        """
        order_by: NotRequired[Literal["created", "effective_at"]]
        """
        The results are in reverse chronological order by `created` or `effective_at`. The default is `created`.
        """
        starting_after: NotRequired[str]
        """
        A cursor for use in pagination. `starting_after` is an object ID that defines your place in the list. For instance, if you make a list request and receive 100 objects, ending with `obj_foo`, your subsequent call can include `starting_after=obj_foo` in order to fetch the next page of the list.
        """
        transaction: NotRequired[str]
        """
        Only return TransactionEntries associated with this Transaction.
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

    class ListParamsEffectiveAt(TypedDict):
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
        params: "TransactionEntryService.ListParams",
        options: RequestOptions = {},
    ) -> ListObject[TransactionEntry]:
        """
        Retrieves a list of TransactionEntry objects.
        """
        return cast(
            ListObject[TransactionEntry],
            self._request(
                "get",
                "/v1/treasury/transaction_entries",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "TransactionEntryService.ListParams",
        options: RequestOptions = {},
    ) -> ListObject[TransactionEntry]:
        """
        Retrieves a list of TransactionEntry objects.
        """
        return cast(
            ListObject[TransactionEntry],
            await self._request_async(
                "get",
                "/v1/treasury/transaction_entries",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        id: str,
        params: "TransactionEntryService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> TransactionEntry:
        """
        Retrieves a TransactionEntry object.
        """
        return cast(
            TransactionEntry,
            self._request(
                "get",
                "/v1/treasury/transaction_entries/{id}".format(
                    id=sanitize_id(id),
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
        params: "TransactionEntryService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> TransactionEntry:
        """
        Retrieves a TransactionEntry object.
        """
        return cast(
            TransactionEntry,
            await self._request_async(
                "get",
                "/v1/treasury/transaction_entries/{id}".format(
                    id=sanitize_id(id),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

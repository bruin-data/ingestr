# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from stripe.issuing._transaction import Transaction
from typing import Dict, List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class TransactionService(StripeService):
    class ListParams(TypedDict):
        card: NotRequired[str]
        """
        Only return transactions that belong to the given card.
        """
        cardholder: NotRequired[str]
        """
        Only return transactions that belong to the given cardholder.
        """
        created: NotRequired["TransactionService.ListParamsCreated|int"]
        """
        Only return transactions that were created during the given date interval.
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
        type: NotRequired[Literal["capture", "refund"]]
        """
        Only return transactions that have the given type. One of `capture` or `refund`.
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

    class UpdateParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """

    def list(
        self,
        params: "TransactionService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Transaction]:
        """
        Returns a list of Issuing Transaction objects. The objects are sorted in descending order by creation date, with the most recently created object appearing first.
        """
        return cast(
            ListObject[Transaction],
            self._request(
                "get",
                "/v1/issuing/transactions",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "TransactionService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Transaction]:
        """
        Returns a list of Issuing Transaction objects. The objects are sorted in descending order by creation date, with the most recently created object appearing first.
        """
        return cast(
            ListObject[Transaction],
            await self._request_async(
                "get",
                "/v1/issuing/transactions",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        transaction: str,
        params: "TransactionService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Transaction:
        """
        Retrieves an Issuing Transaction object.
        """
        return cast(
            Transaction,
            self._request(
                "get",
                "/v1/issuing/transactions/{transaction}".format(
                    transaction=sanitize_id(transaction),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        transaction: str,
        params: "TransactionService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Transaction:
        """
        Retrieves an Issuing Transaction object.
        """
        return cast(
            Transaction,
            await self._request_async(
                "get",
                "/v1/issuing/transactions/{transaction}".format(
                    transaction=sanitize_id(transaction),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def update(
        self,
        transaction: str,
        params: "TransactionService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Transaction:
        """
        Updates the specified Issuing Transaction object by setting the values of the parameters passed. Any parameters not provided will be left unchanged.
        """
        return cast(
            Transaction,
            self._request(
                "post",
                "/v1/issuing/transactions/{transaction}".format(
                    transaction=sanitize_id(transaction),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def update_async(
        self,
        transaction: str,
        params: "TransactionService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Transaction:
        """
        Updates the specified Issuing Transaction object by setting the values of the parameters passed. Any parameters not provided will be left unchanged.
        """
        return cast(
            Transaction,
            await self._request_async(
                "post",
                "/v1/issuing/transactions/{transaction}".format(
                    transaction=sanitize_id(transaction),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

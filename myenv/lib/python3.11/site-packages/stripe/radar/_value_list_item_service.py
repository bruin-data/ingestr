# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from stripe.radar._value_list_item import ValueListItem
from typing import List, cast
from typing_extensions import NotRequired, TypedDict


class ValueListItemService(StripeService):
    class CreateParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        value: str
        """
        The value of the item (whose type must match the type of the parent value list).
        """
        value_list: str
        """
        The identifier of the value list which the created item will be added to.
        """

    class DeleteParams(TypedDict):
        pass

    class ListParams(TypedDict):
        created: NotRequired["ValueListItemService.ListParamsCreated|int"]
        """
        Only return items that were created during the given date interval.
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
        value: NotRequired[str]
        """
        Return items belonging to the parent list whose value matches the specified value (using an "is like" match).
        """
        value_list: str
        """
        Identifier for the parent value list this item belongs to.
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

    def delete(
        self,
        item: str,
        params: "ValueListItemService.DeleteParams" = {},
        options: RequestOptions = {},
    ) -> ValueListItem:
        """
        Deletes a ValueListItem object, removing it from its parent value list.
        """
        return cast(
            ValueListItem,
            self._request(
                "delete",
                "/v1/radar/value_list_items/{item}".format(
                    item=sanitize_id(item),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def delete_async(
        self,
        item: str,
        params: "ValueListItemService.DeleteParams" = {},
        options: RequestOptions = {},
    ) -> ValueListItem:
        """
        Deletes a ValueListItem object, removing it from its parent value list.
        """
        return cast(
            ValueListItem,
            await self._request_async(
                "delete",
                "/v1/radar/value_list_items/{item}".format(
                    item=sanitize_id(item),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        item: str,
        params: "ValueListItemService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> ValueListItem:
        """
        Retrieves a ValueListItem object.
        """
        return cast(
            ValueListItem,
            self._request(
                "get",
                "/v1/radar/value_list_items/{item}".format(
                    item=sanitize_id(item),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        item: str,
        params: "ValueListItemService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> ValueListItem:
        """
        Retrieves a ValueListItem object.
        """
        return cast(
            ValueListItem,
            await self._request_async(
                "get",
                "/v1/radar/value_list_items/{item}".format(
                    item=sanitize_id(item),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def list(
        self,
        params: "ValueListItemService.ListParams",
        options: RequestOptions = {},
    ) -> ListObject[ValueListItem]:
        """
        Returns a list of ValueListItem objects. The objects are sorted in descending order by creation date, with the most recently created object appearing first.
        """
        return cast(
            ListObject[ValueListItem],
            self._request(
                "get",
                "/v1/radar/value_list_items",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "ValueListItemService.ListParams",
        options: RequestOptions = {},
    ) -> ListObject[ValueListItem]:
        """
        Returns a list of ValueListItem objects. The objects are sorted in descending order by creation date, with the most recently created object appearing first.
        """
        return cast(
            ListObject[ValueListItem],
            await self._request_async(
                "get",
                "/v1/radar/value_list_items",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self,
        params: "ValueListItemService.CreateParams",
        options: RequestOptions = {},
    ) -> ValueListItem:
        """
        Creates a new ValueListItem object, which is added to the specified parent value list.
        """
        return cast(
            ValueListItem,
            self._request(
                "post",
                "/v1/radar/value_list_items",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        params: "ValueListItemService.CreateParams",
        options: RequestOptions = {},
    ) -> ValueListItem:
        """
        Creates a new ValueListItem object, which is added to the specified parent value list.
        """
        return cast(
            ValueListItem,
            await self._request_async(
                "post",
                "/v1/radar/value_list_items",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

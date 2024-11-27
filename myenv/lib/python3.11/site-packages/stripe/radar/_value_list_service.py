# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from stripe.radar._value_list import ValueList
from typing import Dict, List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class ValueListService(StripeService):
    class CreateParams(TypedDict):
        alias: str
        """
        The name of the value list for use in rules.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        item_type: NotRequired[
            Literal[
                "card_bin",
                "card_fingerprint",
                "case_sensitive_string",
                "country",
                "customer_id",
                "email",
                "ip_address",
                "sepa_debit_fingerprint",
                "string",
                "us_bank_account_fingerprint",
            ]
        ]
        """
        Type of the items in the value list. One of `card_fingerprint`, `us_bank_account_fingerprint`, `sepa_debit_fingerprint`, `card_bin`, `email`, `ip_address`, `country`, `string`, `case_sensitive_string`, or `customer_id`. Use `string` if the item type is unknown or mixed.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        name: str
        """
        The human-readable name of the value list.
        """

    class DeleteParams(TypedDict):
        pass

    class ListParams(TypedDict):
        alias: NotRequired[str]
        """
        The alias used to reference the value list when writing rules.
        """
        contains: NotRequired[str]
        """
        A value contained within a value list - returns all value lists containing this value.
        """
        created: NotRequired["ValueListService.ListParamsCreated|int"]
        """
        Only return value lists that were created during the given date interval.
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
        alias: NotRequired[str]
        """
        The name of the value list for use in rules.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        name: NotRequired[str]
        """
        The human-readable name of the value list.
        """

    def delete(
        self,
        value_list: str,
        params: "ValueListService.DeleteParams" = {},
        options: RequestOptions = {},
    ) -> ValueList:
        """
        Deletes a ValueList object, also deleting any items contained within the value list. To be deleted, a value list must not be referenced in any rules.
        """
        return cast(
            ValueList,
            self._request(
                "delete",
                "/v1/radar/value_lists/{value_list}".format(
                    value_list=sanitize_id(value_list),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def delete_async(
        self,
        value_list: str,
        params: "ValueListService.DeleteParams" = {},
        options: RequestOptions = {},
    ) -> ValueList:
        """
        Deletes a ValueList object, also deleting any items contained within the value list. To be deleted, a value list must not be referenced in any rules.
        """
        return cast(
            ValueList,
            await self._request_async(
                "delete",
                "/v1/radar/value_lists/{value_list}".format(
                    value_list=sanitize_id(value_list),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        value_list: str,
        params: "ValueListService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> ValueList:
        """
        Retrieves a ValueList object.
        """
        return cast(
            ValueList,
            self._request(
                "get",
                "/v1/radar/value_lists/{value_list}".format(
                    value_list=sanitize_id(value_list),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        value_list: str,
        params: "ValueListService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> ValueList:
        """
        Retrieves a ValueList object.
        """
        return cast(
            ValueList,
            await self._request_async(
                "get",
                "/v1/radar/value_lists/{value_list}".format(
                    value_list=sanitize_id(value_list),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def update(
        self,
        value_list: str,
        params: "ValueListService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> ValueList:
        """
        Updates a ValueList object by setting the values of the parameters passed. Any parameters not provided will be left unchanged. Note that item_type is immutable.
        """
        return cast(
            ValueList,
            self._request(
                "post",
                "/v1/radar/value_lists/{value_list}".format(
                    value_list=sanitize_id(value_list),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def update_async(
        self,
        value_list: str,
        params: "ValueListService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> ValueList:
        """
        Updates a ValueList object by setting the values of the parameters passed. Any parameters not provided will be left unchanged. Note that item_type is immutable.
        """
        return cast(
            ValueList,
            await self._request_async(
                "post",
                "/v1/radar/value_lists/{value_list}".format(
                    value_list=sanitize_id(value_list),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def list(
        self,
        params: "ValueListService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[ValueList]:
        """
        Returns a list of ValueList objects. The objects are sorted in descending order by creation date, with the most recently created object appearing first.
        """
        return cast(
            ListObject[ValueList],
            self._request(
                "get",
                "/v1/radar/value_lists",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "ValueListService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[ValueList]:
        """
        Returns a list of ValueList objects. The objects are sorted in descending order by creation date, with the most recently created object appearing first.
        """
        return cast(
            ListObject[ValueList],
            await self._request_async(
                "get",
                "/v1/radar/value_lists",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self,
        params: "ValueListService.CreateParams",
        options: RequestOptions = {},
    ) -> ValueList:
        """
        Creates a new ValueList object, which can then be referenced in rules.
        """
        return cast(
            ValueList,
            self._request(
                "post",
                "/v1/radar/value_lists",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        params: "ValueListService.CreateParams",
        options: RequestOptions = {},
    ) -> ValueList:
        """
        Creates a new ValueList object, which can then be referenced in rules.
        """
        return cast(
            ValueList,
            await self._request_async(
                "post",
                "/v1/radar/value_lists",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._createable_api_resource import CreateableAPIResource
from stripe._deletable_api_resource import DeletableAPIResource
from stripe._list_object import ListObject
from stripe._listable_api_resource import ListableAPIResource
from stripe._request_options import RequestOptions
from stripe._util import class_method_variant, sanitize_id
from typing import ClassVar, List, Optional, cast, overload
from typing_extensions import Literal, NotRequired, TypedDict, Unpack


class ValueListItem(
    CreateableAPIResource["ValueListItem"],
    DeletableAPIResource["ValueListItem"],
    ListableAPIResource["ValueListItem"],
):
    """
    Value list items allow you to add specific values to a given Radar value list, which can then be used in rules.

    Related guide: [Managing list items](https://stripe.com/docs/radar/lists#managing-list-items)
    """

    OBJECT_NAME: ClassVar[Literal["radar.value_list_item"]] = (
        "radar.value_list_item"
    )

    class CreateParams(RequestOptions):
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

    class DeleteParams(RequestOptions):
        pass

    class ListParams(RequestOptions):
        created: NotRequired["ValueListItem.ListParamsCreated|int"]
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

    class RetrieveParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    created: int
    """
    Time at which the object was created. Measured in seconds since the Unix epoch.
    """
    created_by: str
    """
    The name or email address of the user who added this item to the value list.
    """
    id: str
    """
    Unique identifier for the object.
    """
    livemode: bool
    """
    Has the value `true` if the object exists in live mode or the value `false` if the object exists in test mode.
    """
    object: Literal["radar.value_list_item"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """
    value: str
    """
    The value of the item.
    """
    value_list: str
    """
    The identifier of the value list this item belongs to.
    """
    deleted: Optional[Literal[True]]
    """
    Always true for a deleted object
    """

    @classmethod
    def create(
        cls, **params: Unpack["ValueListItem.CreateParams"]
    ) -> "ValueListItem":
        """
        Creates a new ValueListItem object, which is added to the specified parent value list.
        """
        return cast(
            "ValueListItem",
            cls._static_request(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    async def create_async(
        cls, **params: Unpack["ValueListItem.CreateParams"]
    ) -> "ValueListItem":
        """
        Creates a new ValueListItem object, which is added to the specified parent value list.
        """
        return cast(
            "ValueListItem",
            await cls._static_request_async(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    def _cls_delete(
        cls, sid: str, **params: Unpack["ValueListItem.DeleteParams"]
    ) -> "ValueListItem":
        """
        Deletes a ValueListItem object, removing it from its parent value list.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(sid))
        return cast(
            "ValueListItem",
            cls._static_request(
                "delete",
                url,
                params=params,
            ),
        )

    @overload
    @staticmethod
    def delete(
        sid: str, **params: Unpack["ValueListItem.DeleteParams"]
    ) -> "ValueListItem":
        """
        Deletes a ValueListItem object, removing it from its parent value list.
        """
        ...

    @overload
    def delete(
        self, **params: Unpack["ValueListItem.DeleteParams"]
    ) -> "ValueListItem":
        """
        Deletes a ValueListItem object, removing it from its parent value list.
        """
        ...

    @class_method_variant("_cls_delete")
    def delete(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["ValueListItem.DeleteParams"]
    ) -> "ValueListItem":
        """
        Deletes a ValueListItem object, removing it from its parent value list.
        """
        return self._request_and_refresh(
            "delete",
            self.instance_url(),
            params=params,
        )

    @classmethod
    async def _cls_delete_async(
        cls, sid: str, **params: Unpack["ValueListItem.DeleteParams"]
    ) -> "ValueListItem":
        """
        Deletes a ValueListItem object, removing it from its parent value list.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(sid))
        return cast(
            "ValueListItem",
            await cls._static_request_async(
                "delete",
                url,
                params=params,
            ),
        )

    @overload
    @staticmethod
    async def delete_async(
        sid: str, **params: Unpack["ValueListItem.DeleteParams"]
    ) -> "ValueListItem":
        """
        Deletes a ValueListItem object, removing it from its parent value list.
        """
        ...

    @overload
    async def delete_async(
        self, **params: Unpack["ValueListItem.DeleteParams"]
    ) -> "ValueListItem":
        """
        Deletes a ValueListItem object, removing it from its parent value list.
        """
        ...

    @class_method_variant("_cls_delete_async")
    async def delete_async(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["ValueListItem.DeleteParams"]
    ) -> "ValueListItem":
        """
        Deletes a ValueListItem object, removing it from its parent value list.
        """
        return await self._request_and_refresh_async(
            "delete",
            self.instance_url(),
            params=params,
        )

    @classmethod
    def list(
        cls, **params: Unpack["ValueListItem.ListParams"]
    ) -> ListObject["ValueListItem"]:
        """
        Returns a list of ValueListItem objects. The objects are sorted in descending order by creation date, with the most recently created object appearing first.
        """
        result = cls._static_request(
            "get",
            cls.class_url(),
            params=params,
        )
        if not isinstance(result, ListObject):
            raise TypeError(
                "Expected list object from API, got %s"
                % (type(result).__name__)
            )

        return result

    @classmethod
    async def list_async(
        cls, **params: Unpack["ValueListItem.ListParams"]
    ) -> ListObject["ValueListItem"]:
        """
        Returns a list of ValueListItem objects. The objects are sorted in descending order by creation date, with the most recently created object appearing first.
        """
        result = await cls._static_request_async(
            "get",
            cls.class_url(),
            params=params,
        )
        if not isinstance(result, ListObject):
            raise TypeError(
                "Expected list object from API, got %s"
                % (type(result).__name__)
            )

        return result

    @classmethod
    def retrieve(
        cls, id: str, **params: Unpack["ValueListItem.RetrieveParams"]
    ) -> "ValueListItem":
        """
        Retrieves a ValueListItem object.
        """
        instance = cls(id, **params)
        instance.refresh()
        return instance

    @classmethod
    async def retrieve_async(
        cls, id: str, **params: Unpack["ValueListItem.RetrieveParams"]
    ) -> "ValueListItem":
        """
        Retrieves a ValueListItem object.
        """
        instance = cls(id, **params)
        await instance.refresh_async()
        return instance

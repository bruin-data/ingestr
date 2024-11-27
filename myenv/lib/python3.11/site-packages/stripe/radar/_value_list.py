# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._createable_api_resource import CreateableAPIResource
from stripe._deletable_api_resource import DeletableAPIResource
from stripe._list_object import ListObject
from stripe._listable_api_resource import ListableAPIResource
from stripe._request_options import RequestOptions
from stripe._updateable_api_resource import UpdateableAPIResource
from stripe._util import class_method_variant, sanitize_id
from typing import ClassVar, Dict, List, Optional, cast, overload
from typing_extensions import (
    Literal,
    NotRequired,
    TypedDict,
    Unpack,
    TYPE_CHECKING,
)

if TYPE_CHECKING:
    from stripe.radar._value_list_item import ValueListItem


class ValueList(
    CreateableAPIResource["ValueList"],
    DeletableAPIResource["ValueList"],
    ListableAPIResource["ValueList"],
    UpdateableAPIResource["ValueList"],
):
    """
    Value lists allow you to group values together which can then be referenced in rules.

    Related guide: [Default Stripe lists](https://stripe.com/docs/radar/lists#managing-list-items)
    """

    OBJECT_NAME: ClassVar[Literal["radar.value_list"]] = "radar.value_list"

    class CreateParams(RequestOptions):
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

    class DeleteParams(RequestOptions):
        pass

    class ListParams(RequestOptions):
        alias: NotRequired[str]
        """
        The alias used to reference the value list when writing rules.
        """
        contains: NotRequired[str]
        """
        A value contained within a value list - returns all value lists containing this value.
        """
        created: NotRequired["ValueList.ListParamsCreated|int"]
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

    class ModifyParams(RequestOptions):
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

    class RetrieveParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    alias: str
    """
    The name of the value list for use in rules.
    """
    created: int
    """
    Time at which the object was created. Measured in seconds since the Unix epoch.
    """
    created_by: str
    """
    The name or email address of the user who created this value list.
    """
    id: str
    """
    Unique identifier for the object.
    """
    item_type: Literal[
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
    """
    The type of items in the value list. One of `card_fingerprint`, `us_bank_account_fingerprint`, `sepa_debit_fingerprint`, `card_bin`, `email`, `ip_address`, `country`, `string`, `case_sensitive_string`, or `customer_id`.
    """
    list_items: ListObject["ValueListItem"]
    """
    List of items contained within this value list.
    """
    livemode: bool
    """
    Has the value `true` if the object exists in live mode or the value `false` if the object exists in test mode.
    """
    metadata: Dict[str, str]
    """
    Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format.
    """
    name: str
    """
    The name of the value list.
    """
    object: Literal["radar.value_list"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """
    deleted: Optional[Literal[True]]
    """
    Always true for a deleted object
    """

    @classmethod
    def create(cls, **params: Unpack["ValueList.CreateParams"]) -> "ValueList":
        """
        Creates a new ValueList object, which can then be referenced in rules.
        """
        return cast(
            "ValueList",
            cls._static_request(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    async def create_async(
        cls, **params: Unpack["ValueList.CreateParams"]
    ) -> "ValueList":
        """
        Creates a new ValueList object, which can then be referenced in rules.
        """
        return cast(
            "ValueList",
            await cls._static_request_async(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    def _cls_delete(
        cls, sid: str, **params: Unpack["ValueList.DeleteParams"]
    ) -> "ValueList":
        """
        Deletes a ValueList object, also deleting any items contained within the value list. To be deleted, a value list must not be referenced in any rules.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(sid))
        return cast(
            "ValueList",
            cls._static_request(
                "delete",
                url,
                params=params,
            ),
        )

    @overload
    @staticmethod
    def delete(
        sid: str, **params: Unpack["ValueList.DeleteParams"]
    ) -> "ValueList":
        """
        Deletes a ValueList object, also deleting any items contained within the value list. To be deleted, a value list must not be referenced in any rules.
        """
        ...

    @overload
    def delete(
        self, **params: Unpack["ValueList.DeleteParams"]
    ) -> "ValueList":
        """
        Deletes a ValueList object, also deleting any items contained within the value list. To be deleted, a value list must not be referenced in any rules.
        """
        ...

    @class_method_variant("_cls_delete")
    def delete(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["ValueList.DeleteParams"]
    ) -> "ValueList":
        """
        Deletes a ValueList object, also deleting any items contained within the value list. To be deleted, a value list must not be referenced in any rules.
        """
        return self._request_and_refresh(
            "delete",
            self.instance_url(),
            params=params,
        )

    @classmethod
    async def _cls_delete_async(
        cls, sid: str, **params: Unpack["ValueList.DeleteParams"]
    ) -> "ValueList":
        """
        Deletes a ValueList object, also deleting any items contained within the value list. To be deleted, a value list must not be referenced in any rules.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(sid))
        return cast(
            "ValueList",
            await cls._static_request_async(
                "delete",
                url,
                params=params,
            ),
        )

    @overload
    @staticmethod
    async def delete_async(
        sid: str, **params: Unpack["ValueList.DeleteParams"]
    ) -> "ValueList":
        """
        Deletes a ValueList object, also deleting any items contained within the value list. To be deleted, a value list must not be referenced in any rules.
        """
        ...

    @overload
    async def delete_async(
        self, **params: Unpack["ValueList.DeleteParams"]
    ) -> "ValueList":
        """
        Deletes a ValueList object, also deleting any items contained within the value list. To be deleted, a value list must not be referenced in any rules.
        """
        ...

    @class_method_variant("_cls_delete_async")
    async def delete_async(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["ValueList.DeleteParams"]
    ) -> "ValueList":
        """
        Deletes a ValueList object, also deleting any items contained within the value list. To be deleted, a value list must not be referenced in any rules.
        """
        return await self._request_and_refresh_async(
            "delete",
            self.instance_url(),
            params=params,
        )

    @classmethod
    def list(
        cls, **params: Unpack["ValueList.ListParams"]
    ) -> ListObject["ValueList"]:
        """
        Returns a list of ValueList objects. The objects are sorted in descending order by creation date, with the most recently created object appearing first.
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
        cls, **params: Unpack["ValueList.ListParams"]
    ) -> ListObject["ValueList"]:
        """
        Returns a list of ValueList objects. The objects are sorted in descending order by creation date, with the most recently created object appearing first.
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
    def modify(
        cls, id: str, **params: Unpack["ValueList.ModifyParams"]
    ) -> "ValueList":
        """
        Updates a ValueList object by setting the values of the parameters passed. Any parameters not provided will be left unchanged. Note that item_type is immutable.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(id))
        return cast(
            "ValueList",
            cls._static_request(
                "post",
                url,
                params=params,
            ),
        )

    @classmethod
    async def modify_async(
        cls, id: str, **params: Unpack["ValueList.ModifyParams"]
    ) -> "ValueList":
        """
        Updates a ValueList object by setting the values of the parameters passed. Any parameters not provided will be left unchanged. Note that item_type is immutable.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(id))
        return cast(
            "ValueList",
            await cls._static_request_async(
                "post",
                url,
                params=params,
            ),
        )

    @classmethod
    def retrieve(
        cls, id: str, **params: Unpack["ValueList.RetrieveParams"]
    ) -> "ValueList":
        """
        Retrieves a ValueList object.
        """
        instance = cls(id, **params)
        instance.refresh()
        return instance

    @classmethod
    async def retrieve_async(
        cls, id: str, **params: Unpack["ValueList.RetrieveParams"]
    ) -> "ValueList":
        """
        Retrieves a ValueList object.
        """
        instance = cls(id, **params)
        await instance.refresh_async()
        return instance

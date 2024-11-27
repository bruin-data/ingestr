# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._createable_api_resource import CreateableAPIResource
from stripe._expandable_field import ExpandableField
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
    from stripe._balance_transaction import BalanceTransaction
    from stripe._source import Source


class Topup(
    CreateableAPIResource["Topup"],
    ListableAPIResource["Topup"],
    UpdateableAPIResource["Topup"],
):
    """
    To top up your Stripe balance, you create a top-up object. You can retrieve
    individual top-ups, as well as list all top-ups. Top-ups are identified by a
    unique, random ID.

    Related guide: [Topping up your platform account](https://stripe.com/docs/connect/top-ups)
    """

    OBJECT_NAME: ClassVar[Literal["topup"]] = "topup"

    class CancelParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class CreateParams(RequestOptions):
        amount: int
        """
        A positive integer representing how much to transfer.
        """
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        description: NotRequired[str]
        """
        An arbitrary string attached to the object. Often useful for displaying to users.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        source: NotRequired[str]
        """
        The ID of a source to transfer funds from. For most users, this should be left unspecified which will use the bank account that was set up in the dashboard for the specified currency. In test mode, this can be a test bank token (see [Testing Top-ups](https://stripe.com/docs/connect/testing#testing-top-ups)).
        """
        statement_descriptor: NotRequired[str]
        """
        Extra information about a top-up for the source's bank statement. Limited to 15 ASCII characters.
        """
        transfer_group: NotRequired[str]
        """
        A string that identifies this top-up as part of a group.
        """

    class ListParams(RequestOptions):
        amount: NotRequired["Topup.ListParamsAmount|int"]
        """
        A positive integer representing how much to transfer.
        """
        created: NotRequired["Topup.ListParamsCreated|int"]
        """
        A filter on the list, based on the object `created` field. The value can be a string with an integer Unix timestamp, or it can be a dictionary with a number of different query options.
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
        status: NotRequired[
            Literal["canceled", "failed", "pending", "succeeded"]
        ]
        """
        Only return top-ups that have the given status. One of `canceled`, `failed`, `pending` or `succeeded`.
        """

    class ListParamsAmount(TypedDict):
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
        description: NotRequired[str]
        """
        An arbitrary string attached to the object. Often useful for displaying to users.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """

    class RetrieveParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    amount: int
    """
    Amount transferred.
    """
    balance_transaction: Optional[ExpandableField["BalanceTransaction"]]
    """
    ID of the balance transaction that describes the impact of this top-up on your account balance. May not be specified depending on status of top-up.
    """
    created: int
    """
    Time at which the object was created. Measured in seconds since the Unix epoch.
    """
    currency: str
    """
    Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
    """
    description: Optional[str]
    """
    An arbitrary string attached to the object. Often useful for displaying to users.
    """
    expected_availability_date: Optional[int]
    """
    Date the funds are expected to arrive in your Stripe account for payouts. This factors in delays like weekends or bank holidays. May not be specified depending on status of top-up.
    """
    failure_code: Optional[str]
    """
    Error code explaining reason for top-up failure if available (see [the errors section](https://stripe.com/docs/api#errors) for a list of codes).
    """
    failure_message: Optional[str]
    """
    Message to user further explaining reason for top-up failure if available.
    """
    id: str
    """
    Unique identifier for the object.
    """
    livemode: bool
    """
    Has the value `true` if the object exists in live mode or the value `false` if the object exists in test mode.
    """
    metadata: Dict[str, str]
    """
    Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format.
    """
    object: Literal["topup"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """
    source: Optional["Source"]
    """
    The source field is deprecated. It might not always be present in the API response.
    """
    statement_descriptor: Optional[str]
    """
    Extra information about a top-up. This will appear on your source's bank statement. It must contain at least one letter.
    """
    status: Literal["canceled", "failed", "pending", "reversed", "succeeded"]
    """
    The status of the top-up is either `canceled`, `failed`, `pending`, `reversed`, or `succeeded`.
    """
    transfer_group: Optional[str]
    """
    A string that identifies this top-up as part of a group.
    """

    @classmethod
    def _cls_cancel(
        cls, topup: str, **params: Unpack["Topup.CancelParams"]
    ) -> "Topup":
        """
        Cancels a top-up. Only pending top-ups can be canceled.
        """
        return cast(
            "Topup",
            cls._static_request(
                "post",
                "/v1/topups/{topup}/cancel".format(topup=sanitize_id(topup)),
                params=params,
            ),
        )

    @overload
    @staticmethod
    def cancel(topup: str, **params: Unpack["Topup.CancelParams"]) -> "Topup":
        """
        Cancels a top-up. Only pending top-ups can be canceled.
        """
        ...

    @overload
    def cancel(self, **params: Unpack["Topup.CancelParams"]) -> "Topup":
        """
        Cancels a top-up. Only pending top-ups can be canceled.
        """
        ...

    @class_method_variant("_cls_cancel")
    def cancel(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Topup.CancelParams"]
    ) -> "Topup":
        """
        Cancels a top-up. Only pending top-ups can be canceled.
        """
        return cast(
            "Topup",
            self._request(
                "post",
                "/v1/topups/{topup}/cancel".format(
                    topup=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    async def _cls_cancel_async(
        cls, topup: str, **params: Unpack["Topup.CancelParams"]
    ) -> "Topup":
        """
        Cancels a top-up. Only pending top-ups can be canceled.
        """
        return cast(
            "Topup",
            await cls._static_request_async(
                "post",
                "/v1/topups/{topup}/cancel".format(topup=sanitize_id(topup)),
                params=params,
            ),
        )

    @overload
    @staticmethod
    async def cancel_async(
        topup: str, **params: Unpack["Topup.CancelParams"]
    ) -> "Topup":
        """
        Cancels a top-up. Only pending top-ups can be canceled.
        """
        ...

    @overload
    async def cancel_async(
        self, **params: Unpack["Topup.CancelParams"]
    ) -> "Topup":
        """
        Cancels a top-up. Only pending top-ups can be canceled.
        """
        ...

    @class_method_variant("_cls_cancel_async")
    async def cancel_async(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Topup.CancelParams"]
    ) -> "Topup":
        """
        Cancels a top-up. Only pending top-ups can be canceled.
        """
        return cast(
            "Topup",
            await self._request_async(
                "post",
                "/v1/topups/{topup}/cancel".format(
                    topup=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    def create(cls, **params: Unpack["Topup.CreateParams"]) -> "Topup":
        """
        Top up the balance of an account
        """
        return cast(
            "Topup",
            cls._static_request(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    async def create_async(
        cls, **params: Unpack["Topup.CreateParams"]
    ) -> "Topup":
        """
        Top up the balance of an account
        """
        return cast(
            "Topup",
            await cls._static_request_async(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    def list(cls, **params: Unpack["Topup.ListParams"]) -> ListObject["Topup"]:
        """
        Returns a list of top-ups.
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
        cls, **params: Unpack["Topup.ListParams"]
    ) -> ListObject["Topup"]:
        """
        Returns a list of top-ups.
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
        cls, id: str, **params: Unpack["Topup.ModifyParams"]
    ) -> "Topup":
        """
        Updates the metadata of a top-up. Other top-up details are not editable by design.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(id))
        return cast(
            "Topup",
            cls._static_request(
                "post",
                url,
                params=params,
            ),
        )

    @classmethod
    async def modify_async(
        cls, id: str, **params: Unpack["Topup.ModifyParams"]
    ) -> "Topup":
        """
        Updates the metadata of a top-up. Other top-up details are not editable by design.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(id))
        return cast(
            "Topup",
            await cls._static_request_async(
                "post",
                url,
                params=params,
            ),
        )

    @classmethod
    def retrieve(
        cls, id: str, **params: Unpack["Topup.RetrieveParams"]
    ) -> "Topup":
        """
        Retrieves the details of a top-up that has previously been created. Supply the unique top-up ID that was returned from your previous request, and Stripe will return the corresponding top-up information.
        """
        instance = cls(id, **params)
        instance.refresh()
        return instance

    @classmethod
    async def retrieve_async(
        cls, id: str, **params: Unpack["Topup.RetrieveParams"]
    ) -> "Topup":
        """
        Retrieves the details of a top-up that has previously been created. Supply the unique top-up ID that was returned from your previous request, and Stripe will return the corresponding top-up information.
        """
        instance = cls(id, **params)
        await instance.refresh_async()
        return instance

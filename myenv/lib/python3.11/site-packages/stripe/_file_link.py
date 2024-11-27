# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._createable_api_resource import CreateableAPIResource
from stripe._expandable_field import ExpandableField
from stripe._list_object import ListObject
from stripe._listable_api_resource import ListableAPIResource
from stripe._request_options import RequestOptions
from stripe._updateable_api_resource import UpdateableAPIResource
from stripe._util import sanitize_id
from typing import ClassVar, Dict, List, Optional, cast
from typing_extensions import (
    Literal,
    NotRequired,
    TypedDict,
    Unpack,
    TYPE_CHECKING,
)

if TYPE_CHECKING:
    from stripe._file import File


class FileLink(
    CreateableAPIResource["FileLink"],
    ListableAPIResource["FileLink"],
    UpdateableAPIResource["FileLink"],
):
    """
    To share the contents of a `File` object with non-Stripe users, you can
    create a `FileLink`. `FileLink`s contain a URL that you can use to
    retrieve the contents of the file without authentication.
    """

    OBJECT_NAME: ClassVar[Literal["file_link"]] = "file_link"

    class CreateParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        expires_at: NotRequired[int]
        """
        The link isn't usable after this future timestamp.
        """
        file: str
        """
        The ID of the file. The file's `purpose` must be one of the following: `business_icon`, `business_logo`, `customer_signature`, `dispute_evidence`, `finance_report_run`, `identity_document_downloadable`, `pci_document`, `selfie`, `sigma_scheduled_query`, `tax_document_user_upload`, or `terminal_reader_splashscreen`.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """

    class ListParams(RequestOptions):
        created: NotRequired["FileLink.ListParamsCreated|int"]
        """
        Only return links that were created during the given date interval.
        """
        ending_before: NotRequired[str]
        """
        A cursor for use in pagination. `ending_before` is an object ID that defines your place in the list. For instance, if you make a list request and receive 100 objects, starting with `obj_bar`, your subsequent call can include `ending_before=obj_bar` in order to fetch the previous page of the list.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        expired: NotRequired[bool]
        """
        Filter links by their expiration status. By default, Stripe returns all links.
        """
        file: NotRequired[str]
        """
        Only return links for the given file.
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
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        expires_at: NotRequired["Literal['']|Literal['now']|int"]
        """
        A future timestamp after which the link will no longer be usable, or `now` to expire the link immediately.
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

    created: int
    """
    Time at which the object was created. Measured in seconds since the Unix epoch.
    """
    expired: bool
    """
    Returns if the link is already expired.
    """
    expires_at: Optional[int]
    """
    Time that the link expires.
    """
    file: ExpandableField["File"]
    """
    The file object this link points to.
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
    object: Literal["file_link"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """
    url: Optional[str]
    """
    The publicly accessible URL to download the file.
    """

    @classmethod
    def create(cls, **params: Unpack["FileLink.CreateParams"]) -> "FileLink":
        """
        Creates a new file link object.
        """
        return cast(
            "FileLink",
            cls._static_request(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    async def create_async(
        cls, **params: Unpack["FileLink.CreateParams"]
    ) -> "FileLink":
        """
        Creates a new file link object.
        """
        return cast(
            "FileLink",
            await cls._static_request_async(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    def list(
        cls, **params: Unpack["FileLink.ListParams"]
    ) -> ListObject["FileLink"]:
        """
        Returns a list of file links.
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
        cls, **params: Unpack["FileLink.ListParams"]
    ) -> ListObject["FileLink"]:
        """
        Returns a list of file links.
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
        cls, id: str, **params: Unpack["FileLink.ModifyParams"]
    ) -> "FileLink":
        """
        Updates an existing file link object. Expired links can no longer be updated.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(id))
        return cast(
            "FileLink",
            cls._static_request(
                "post",
                url,
                params=params,
            ),
        )

    @classmethod
    async def modify_async(
        cls, id: str, **params: Unpack["FileLink.ModifyParams"]
    ) -> "FileLink":
        """
        Updates an existing file link object. Expired links can no longer be updated.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(id))
        return cast(
            "FileLink",
            await cls._static_request_async(
                "post",
                url,
                params=params,
            ),
        )

    @classmethod
    def retrieve(
        cls, id: str, **params: Unpack["FileLink.RetrieveParams"]
    ) -> "FileLink":
        """
        Retrieves the file link with the given ID.
        """
        instance = cls(id, **params)
        instance.refresh()
        return instance

    @classmethod
    async def retrieve_async(
        cls, id: str, **params: Unpack["FileLink.RetrieveParams"]
    ) -> "FileLink":
        """
        Retrieves the file link with the given ID.
        """
        instance = cls(id, **params)
        await instance.refresh_async()
        return instance

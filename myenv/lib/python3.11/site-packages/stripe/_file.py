# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._createable_api_resource import CreateableAPIResource
from stripe._list_object import ListObject
from stripe._listable_api_resource import ListableAPIResource
from stripe._request_options import RequestOptions
from typing import Any, ClassVar, Dict, List, Optional, cast
from typing_extensions import (
    Literal,
    NotRequired,
    TypedDict,
    Unpack,
    TYPE_CHECKING,
)

if TYPE_CHECKING:
    from stripe._file_link import FileLink


class File(CreateableAPIResource["File"], ListableAPIResource["File"]):
    """
    This object represents files hosted on Stripe's servers. You can upload
    files with the [create file](https://stripe.com/docs/api#create_file) request
    (for example, when uploading dispute evidence). Stripe also
    creates files independently (for example, the results of a [Sigma scheduled
    query](https://stripe.com/docs/api#scheduled_queries)).

    Related guide: [File upload guide](https://stripe.com/docs/file-upload)
    """

    OBJECT_NAME: ClassVar[Literal["file"]] = "file"

    class CreateParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        file: Any
        """
        A file to upload. Make sure that the specifications follow RFC 2388, which defines file transfers for the `multipart/form-data` protocol.
        """
        file_link_data: NotRequired["File.CreateParamsFileLinkData"]
        """
        Optional parameters that automatically create a [file link](https://stripe.com/docs/api#file_links) for the newly created file.
        """
        purpose: Literal[
            "account_requirement",
            "additional_verification",
            "business_icon",
            "business_logo",
            "customer_signature",
            "dispute_evidence",
            "identity_document",
            "pci_document",
            "tax_document_user_upload",
            "terminal_reader_splashscreen",
        ]
        """
        The [purpose](https://stripe.com/docs/file-upload#uploading-a-file) of the uploaded file.
        """

    class CreateParamsFileLinkData(TypedDict):
        create: bool
        """
        Set this to `true` to create a file link for the newly created file. Creating a link is only possible when the file's `purpose` is one of the following: `business_icon`, `business_logo`, `customer_signature`, `dispute_evidence`, `pci_document`, `tax_document_user_upload`, or `terminal_reader_splashscreen`.
        """
        expires_at: NotRequired[int]
        """
        The link isn't available after this future timestamp.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """

    class ListParams(RequestOptions):
        created: NotRequired["File.ListParamsCreated|int"]
        """
        Only return files that were created during the given date interval.
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
        purpose: NotRequired[
            Literal[
                "account_requirement",
                "additional_verification",
                "business_icon",
                "business_logo",
                "customer_signature",
                "dispute_evidence",
                "document_provider_identity_document",
                "finance_report_run",
                "identity_document",
                "identity_document_downloadable",
                "pci_document",
                "selfie",
                "sigma_scheduled_query",
                "tax_document_user_upload",
                "terminal_reader_splashscreen",
            ]
        ]
        """
        Filter queries by the file purpose. If you don't provide a purpose, the queries return unfiltered files.
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

    class RetrieveParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    created: int
    """
    Time at which the object was created. Measured in seconds since the Unix epoch.
    """
    expires_at: Optional[int]
    """
    The file expires and isn't available at this time in epoch seconds.
    """
    filename: Optional[str]
    """
    The suitable name for saving the file to a filesystem.
    """
    id: str
    """
    Unique identifier for the object.
    """
    links: Optional[ListObject["FileLink"]]
    """
    A list of [file links](https://stripe.com/docs/api#file_links) that point at this file.
    """
    object: Literal["file"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """
    purpose: Literal[
        "account_requirement",
        "additional_verification",
        "business_icon",
        "business_logo",
        "customer_signature",
        "dispute_evidence",
        "document_provider_identity_document",
        "finance_report_run",
        "identity_document",
        "identity_document_downloadable",
        "pci_document",
        "selfie",
        "sigma_scheduled_query",
        "tax_document_user_upload",
        "terminal_reader_splashscreen",
    ]
    """
    The [purpose](https://stripe.com/docs/file-upload#uploading-a-file) of the uploaded file.
    """
    size: int
    """
    The size of the file object in bytes.
    """
    title: Optional[str]
    """
    A suitable title for the document.
    """
    type: Optional[str]
    """
    The returned file type (for example, `csv`, `pdf`, `jpg`, or `png`).
    """
    url: Optional[str]
    """
    Use your live secret API key to download the file from this URL.
    """

    @classmethod
    def create(cls, **params: Unpack["File.CreateParams"]) -> "File":
        """
        To upload a file to Stripe, you need to send a request of type multipart/form-data. Include the file you want to upload in the request, and the parameters for creating a file.

        All of Stripe's officially supported Client libraries support sending multipart/form-data.
        """
        return cast(
            "File",
            cls._static_request(
                "post",
                cls.class_url(),
                params=params,
                base_address="files",
                api_mode="V1FILES",
            ),
        )

    @classmethod
    async def create_async(
        cls, **params: Unpack["File.CreateParams"]
    ) -> "File":
        """
        To upload a file to Stripe, you need to send a request of type multipart/form-data. Include the file you want to upload in the request, and the parameters for creating a file.

        All of Stripe's officially supported Client libraries support sending multipart/form-data.
        """
        return cast(
            "File",
            await cls._static_request_async(
                "post",
                cls.class_url(),
                params=params,
                base_address="files",
                api_mode="V1FILES",
            ),
        )

    @classmethod
    def list(cls, **params: Unpack["File.ListParams"]) -> ListObject["File"]:
        """
        Returns a list of the files that your account has access to. Stripe sorts and returns the files by their creation dates, placing the most recently created files at the top.
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
        cls, **params: Unpack["File.ListParams"]
    ) -> ListObject["File"]:
        """
        Returns a list of the files that your account has access to. Stripe sorts and returns the files by their creation dates, placing the most recently created files at the top.
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
        cls, id: str, **params: Unpack["File.RetrieveParams"]
    ) -> "File":
        """
        Retrieves the details of an existing file object. After you supply a unique file ID, Stripe returns the corresponding file object. Learn how to [access file contents](https://stripe.com/docs/file-upload#download-file-contents).
        """
        instance = cls(id, **params)
        instance.refresh()
        return instance

    @classmethod
    async def retrieve_async(
        cls, id: str, **params: Unpack["File.RetrieveParams"]
    ) -> "File":
        """
        Retrieves the details of an existing file object. After you supply a unique file ID, Stripe returns the corresponding file object. Learn how to [access file contents](https://stripe.com/docs/file-upload#download-file-contents).
        """
        instance = cls(id, **params)
        await instance.refresh_async()
        return instance

    # This resource can have two different object names. In latter API
    # versions, only `file` is used, but since stripe-python may be used with
    # any API version, we need to support deserializing the older
    # `file_upload` object into the same class.
    OBJECT_NAME_ALT = "file_upload"

    @classmethod
    def class_url(cls):
        return "/v1/files"


# For backwards compatibility, the `File` class is aliased to `FileUpload`.
FileUpload = File

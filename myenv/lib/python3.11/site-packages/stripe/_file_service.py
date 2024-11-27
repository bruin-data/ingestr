# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._file import File
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from typing import Any, Dict, List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class FileService(StripeService):
    class CreateParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        file: Any
        """
        A file to upload. Make sure that the specifications follow RFC 2388, which defines file transfers for the `multipart/form-data` protocol.
        """
        file_link_data: NotRequired["FileService.CreateParamsFileLinkData"]
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

    class ListParams(TypedDict):
        created: NotRequired["FileService.ListParamsCreated|int"]
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

    class RetrieveParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    def list(
        self,
        params: "FileService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[File]:
        """
        Returns a list of the files that your account has access to. Stripe sorts and returns the files by their creation dates, placing the most recently created files at the top.
        """
        return cast(
            ListObject[File],
            self._request(
                "get",
                "/v1/files",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "FileService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[File]:
        """
        Returns a list of the files that your account has access to. Stripe sorts and returns the files by their creation dates, placing the most recently created files at the top.
        """
        return cast(
            ListObject[File],
            await self._request_async(
                "get",
                "/v1/files",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self, params: "FileService.CreateParams", options: RequestOptions = {}
    ) -> File:
        """
        To upload a file to Stripe, you need to send a request of type multipart/form-data. Include the file you want to upload in the request, and the parameters for creating a file.

        All of Stripe's officially supported Client libraries support sending multipart/form-data.
        """
        return cast(
            File,
            self._request(
                "post",
                "/v1/files",
                api_mode="V1FILES",
                base_address="files",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self, params: "FileService.CreateParams", options: RequestOptions = {}
    ) -> File:
        """
        To upload a file to Stripe, you need to send a request of type multipart/form-data. Include the file you want to upload in the request, and the parameters for creating a file.

        All of Stripe's officially supported Client libraries support sending multipart/form-data.
        """
        return cast(
            File,
            await self._request_async(
                "post",
                "/v1/files",
                api_mode="V1FILES",
                base_address="files",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        file: str,
        params: "FileService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> File:
        """
        Retrieves the details of an existing file object. After you supply a unique file ID, Stripe returns the corresponding file object. Learn how to [access file contents](https://stripe.com/docs/file-upload#download-file-contents).
        """
        return cast(
            File,
            self._request(
                "get",
                "/v1/files/{file}".format(file=sanitize_id(file)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        file: str,
        params: "FileService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> File:
        """
        Retrieves the details of an existing file object. After you supply a unique file ID, Stripe returns the corresponding file object. Learn how to [access file contents](https://stripe.com/docs/file-upload#download-file-contents).
        """
        return cast(
            File,
            await self._request_async(
                "get",
                "/v1/files/{file}".format(file=sanitize_id(file)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

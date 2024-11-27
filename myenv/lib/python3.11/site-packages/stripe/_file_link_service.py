# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._file_link import FileLink
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from typing import Dict, List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class FileLinkService(StripeService):
    class CreateParams(TypedDict):
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

    class ListParams(TypedDict):
        created: NotRequired["FileLinkService.ListParamsCreated|int"]
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
        expires_at: NotRequired["Literal['']|Literal['now']|int"]
        """
        A future timestamp after which the link will no longer be usable, or `now` to expire the link immediately.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """

    def list(
        self,
        params: "FileLinkService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[FileLink]:
        """
        Returns a list of file links.
        """
        return cast(
            ListObject[FileLink],
            self._request(
                "get",
                "/v1/file_links",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "FileLinkService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[FileLink]:
        """
        Returns a list of file links.
        """
        return cast(
            ListObject[FileLink],
            await self._request_async(
                "get",
                "/v1/file_links",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self,
        params: "FileLinkService.CreateParams",
        options: RequestOptions = {},
    ) -> FileLink:
        """
        Creates a new file link object.
        """
        return cast(
            FileLink,
            self._request(
                "post",
                "/v1/file_links",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        params: "FileLinkService.CreateParams",
        options: RequestOptions = {},
    ) -> FileLink:
        """
        Creates a new file link object.
        """
        return cast(
            FileLink,
            await self._request_async(
                "post",
                "/v1/file_links",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        link: str,
        params: "FileLinkService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> FileLink:
        """
        Retrieves the file link with the given ID.
        """
        return cast(
            FileLink,
            self._request(
                "get",
                "/v1/file_links/{link}".format(link=sanitize_id(link)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        link: str,
        params: "FileLinkService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> FileLink:
        """
        Retrieves the file link with the given ID.
        """
        return cast(
            FileLink,
            await self._request_async(
                "get",
                "/v1/file_links/{link}".format(link=sanitize_id(link)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def update(
        self,
        link: str,
        params: "FileLinkService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> FileLink:
        """
        Updates an existing file link object. Expired links can no longer be updated.
        """
        return cast(
            FileLink,
            self._request(
                "post",
                "/v1/file_links/{link}".format(link=sanitize_id(link)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def update_async(
        self,
        link: str,
        params: "FileLinkService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> FileLink:
        """
        Updates an existing file link object. Expired links can no longer be updated.
        """
        return cast(
            FileLink,
            await self._request_async(
                "post",
                "/v1/file_links/{link}".format(link=sanitize_id(link)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

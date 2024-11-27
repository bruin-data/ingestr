# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._capability import Capability
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from typing import List, cast
from typing_extensions import NotRequired, TypedDict


class AccountCapabilityService(StripeService):
    class ListParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
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
        requested: NotRequired[bool]
        """
        To request a new capability for an account, pass true. There can be a delay before the requested capability becomes active. If the capability has any activation requirements, the response includes them in the `requirements` arrays.

        If a capability isn't permanent, you can remove it from the account by passing false. Some capabilities are permanent after they've been requested. Attempting to remove a permanent capability returns an error.
        """

    def list(
        self,
        account: str,
        params: "AccountCapabilityService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Capability]:
        """
        Returns a list of capabilities associated with the account. The capabilities are returned sorted by creation date, with the most recent capability appearing first.
        """
        return cast(
            ListObject[Capability],
            self._request(
                "get",
                "/v1/accounts/{account}/capabilities".format(
                    account=sanitize_id(account),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        account: str,
        params: "AccountCapabilityService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Capability]:
        """
        Returns a list of capabilities associated with the account. The capabilities are returned sorted by creation date, with the most recent capability appearing first.
        """
        return cast(
            ListObject[Capability],
            await self._request_async(
                "get",
                "/v1/accounts/{account}/capabilities".format(
                    account=sanitize_id(account),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        account: str,
        capability: str,
        params: "AccountCapabilityService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Capability:
        """
        Retrieves information about the specified Account Capability.
        """
        return cast(
            Capability,
            self._request(
                "get",
                "/v1/accounts/{account}/capabilities/{capability}".format(
                    account=sanitize_id(account),
                    capability=sanitize_id(capability),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        account: str,
        capability: str,
        params: "AccountCapabilityService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Capability:
        """
        Retrieves information about the specified Account Capability.
        """
        return cast(
            Capability,
            await self._request_async(
                "get",
                "/v1/accounts/{account}/capabilities/{capability}".format(
                    account=sanitize_id(account),
                    capability=sanitize_id(capability),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def update(
        self,
        account: str,
        capability: str,
        params: "AccountCapabilityService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Capability:
        """
        Updates an existing Account Capability. Request or remove a capability by updating its requested parameter.
        """
        return cast(
            Capability,
            self._request(
                "post",
                "/v1/accounts/{account}/capabilities/{capability}".format(
                    account=sanitize_id(account),
                    capability=sanitize_id(capability),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def update_async(
        self,
        account: str,
        capability: str,
        params: "AccountCapabilityService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Capability:
        """
        Updates an existing Account Capability. Request or remove a capability by updating its requested parameter.
        """
        return cast(
            Capability,
            await self._request_async(
                "post",
                "/v1/accounts/{account}/capabilities/{capability}".format(
                    account=sanitize_id(account),
                    capability=sanitize_id(capability),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

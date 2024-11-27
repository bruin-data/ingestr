# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe.apps._secret import Secret
from typing import List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class SecretService(StripeService):
    class CreateParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        expires_at: NotRequired[int]
        """
        The Unix timestamp for the expiry time of the secret, after which the secret deletes.
        """
        name: str
        """
        A name for the secret that's unique within the scope.
        """
        payload: str
        """
        The plaintext secret value to be stored.
        """
        scope: "SecretService.CreateParamsScope"
        """
        Specifies the scoping of the secret. Requests originating from UI extensions can only access account-scoped secrets or secrets scoped to their own user.
        """

    class CreateParamsScope(TypedDict):
        type: Literal["account", "user"]
        """
        The secret scope type.
        """
        user: NotRequired[str]
        """
        The user ID. This field is required if `type` is set to `user`, and should not be provided if `type` is set to `account`.
        """

    class DeleteWhereParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        name: str
        """
        A name for the secret that's unique within the scope.
        """
        scope: "SecretService.DeleteWhereParamsScope"
        """
        Specifies the scoping of the secret. Requests originating from UI extensions can only access account-scoped secrets or secrets scoped to their own user.
        """

    class DeleteWhereParamsScope(TypedDict):
        type: Literal["account", "user"]
        """
        The secret scope type.
        """
        user: NotRequired[str]
        """
        The user ID. This field is required if `type` is set to `user`, and should not be provided if `type` is set to `account`.
        """

    class FindParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        name: str
        """
        A name for the secret that's unique within the scope.
        """
        scope: "SecretService.FindParamsScope"
        """
        Specifies the scoping of the secret. Requests originating from UI extensions can only access account-scoped secrets or secrets scoped to their own user.
        """

    class FindParamsScope(TypedDict):
        type: Literal["account", "user"]
        """
        The secret scope type.
        """
        user: NotRequired[str]
        """
        The user ID. This field is required if `type` is set to `user`, and should not be provided if `type` is set to `account`.
        """

    class ListParams(TypedDict):
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
        scope: "SecretService.ListParamsScope"
        """
        Specifies the scoping of the secret. Requests originating from UI extensions can only access account-scoped secrets or secrets scoped to their own user.
        """
        starting_after: NotRequired[str]
        """
        A cursor for use in pagination. `starting_after` is an object ID that defines your place in the list. For instance, if you make a list request and receive 100 objects, ending with `obj_foo`, your subsequent call can include `starting_after=obj_foo` in order to fetch the next page of the list.
        """

    class ListParamsScope(TypedDict):
        type: Literal["account", "user"]
        """
        The secret scope type.
        """
        user: NotRequired[str]
        """
        The user ID. This field is required if `type` is set to `user`, and should not be provided if `type` is set to `account`.
        """

    def list(
        self, params: "SecretService.ListParams", options: RequestOptions = {}
    ) -> ListObject[Secret]:
        """
        List all secrets stored on the given scope.
        """
        return cast(
            ListObject[Secret],
            self._request(
                "get",
                "/v1/apps/secrets",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self, params: "SecretService.ListParams", options: RequestOptions = {}
    ) -> ListObject[Secret]:
        """
        List all secrets stored on the given scope.
        """
        return cast(
            ListObject[Secret],
            await self._request_async(
                "get",
                "/v1/apps/secrets",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self,
        params: "SecretService.CreateParams",
        options: RequestOptions = {},
    ) -> Secret:
        """
        Create or replace a secret in the secret store.
        """
        return cast(
            Secret,
            self._request(
                "post",
                "/v1/apps/secrets",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        params: "SecretService.CreateParams",
        options: RequestOptions = {},
    ) -> Secret:
        """
        Create or replace a secret in the secret store.
        """
        return cast(
            Secret,
            await self._request_async(
                "post",
                "/v1/apps/secrets",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def find(
        self, params: "SecretService.FindParams", options: RequestOptions = {}
    ) -> Secret:
        """
        Finds a secret in the secret store by name and scope.
        """
        return cast(
            Secret,
            self._request(
                "get",
                "/v1/apps/secrets/find",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def find_async(
        self, params: "SecretService.FindParams", options: RequestOptions = {}
    ) -> Secret:
        """
        Finds a secret in the secret store by name and scope.
        """
        return cast(
            Secret,
            await self._request_async(
                "get",
                "/v1/apps/secrets/find",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def delete_where(
        self,
        params: "SecretService.DeleteWhereParams",
        options: RequestOptions = {},
    ) -> Secret:
        """
        Deletes a secret from the secret store by name and scope.
        """
        return cast(
            Secret,
            self._request(
                "post",
                "/v1/apps/secrets/delete",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def delete_where_async(
        self,
        params: "SecretService.DeleteWhereParams",
        options: RequestOptions = {},
    ) -> Secret:
        """
        Deletes a secret from the secret store by name and scope.
        """
        return cast(
            Secret,
            await self._request_async(
                "post",
                "/v1/apps/secrets/delete",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

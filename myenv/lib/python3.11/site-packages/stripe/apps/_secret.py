# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._createable_api_resource import CreateableAPIResource
from stripe._list_object import ListObject
from stripe._listable_api_resource import ListableAPIResource
from stripe._request_options import RequestOptions
from stripe._stripe_object import StripeObject
from typing import ClassVar, List, Optional, cast
from typing_extensions import Literal, NotRequired, TypedDict, Unpack


class Secret(CreateableAPIResource["Secret"], ListableAPIResource["Secret"]):
    """
    Secret Store is an API that allows Stripe Apps developers to securely persist secrets for use by UI Extensions and app backends.

    The primary resource in Secret Store is a `secret`. Other apps can't view secrets created by an app. Additionally, secrets are scoped to provide further permission control.

    All Dashboard users and the app backend share `account` scoped secrets. Use the `account` scope for secrets that don't change per-user, like a third-party API key.

    A `user` scoped secret is accessible by the app backend and one specific Dashboard user. Use the `user` scope for per-user secrets like per-user OAuth tokens, where different users might have different permissions.

    Related guide: [Store data between page reloads](https://stripe.com/docs/stripe-apps/store-auth-data-custom-objects)
    """

    OBJECT_NAME: ClassVar[Literal["apps.secret"]] = "apps.secret"

    class Scope(StripeObject):
        type: Literal["account", "user"]
        """
        The secret scope type.
        """
        user: Optional[str]
        """
        The user ID, if type is set to "user"
        """

    class CreateParams(RequestOptions):
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
        scope: "Secret.CreateParamsScope"
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

    class DeleteWhereParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        name: str
        """
        A name for the secret that's unique within the scope.
        """
        scope: "Secret.DeleteWhereParamsScope"
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

    class FindParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        name: str
        """
        A name for the secret that's unique within the scope.
        """
        scope: "Secret.FindParamsScope"
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

    class ListParams(RequestOptions):
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
        scope: "Secret.ListParamsScope"
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

    created: int
    """
    Time at which the object was created. Measured in seconds since the Unix epoch.
    """
    deleted: Optional[bool]
    """
    If true, indicates that this secret has been deleted
    """
    expires_at: Optional[int]
    """
    The Unix timestamp for the expiry time of the secret, after which the secret deletes.
    """
    id: str
    """
    Unique identifier for the object.
    """
    livemode: bool
    """
    Has the value `true` if the object exists in live mode or the value `false` if the object exists in test mode.
    """
    name: str
    """
    A name for the secret that's unique within the scope.
    """
    object: Literal["apps.secret"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """
    payload: Optional[str]
    """
    The plaintext secret value to be stored.
    """
    scope: Scope

    @classmethod
    def create(cls, **params: Unpack["Secret.CreateParams"]) -> "Secret":
        """
        Create or replace a secret in the secret store.
        """
        return cast(
            "Secret",
            cls._static_request(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    async def create_async(
        cls, **params: Unpack["Secret.CreateParams"]
    ) -> "Secret":
        """
        Create or replace a secret in the secret store.
        """
        return cast(
            "Secret",
            await cls._static_request_async(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    def delete_where(
        cls, **params: Unpack["Secret.DeleteWhereParams"]
    ) -> "Secret":
        """
        Deletes a secret from the secret store by name and scope.
        """
        return cast(
            "Secret",
            cls._static_request(
                "post",
                "/v1/apps/secrets/delete",
                params=params,
            ),
        )

    @classmethod
    async def delete_where_async(
        cls, **params: Unpack["Secret.DeleteWhereParams"]
    ) -> "Secret":
        """
        Deletes a secret from the secret store by name and scope.
        """
        return cast(
            "Secret",
            await cls._static_request_async(
                "post",
                "/v1/apps/secrets/delete",
                params=params,
            ),
        )

    @classmethod
    def find(cls, **params: Unpack["Secret.FindParams"]) -> "Secret":
        """
        Finds a secret in the secret store by name and scope.
        """
        return cast(
            "Secret",
            cls._static_request(
                "get",
                "/v1/apps/secrets/find",
                params=params,
            ),
        )

    @classmethod
    async def find_async(
        cls, **params: Unpack["Secret.FindParams"]
    ) -> "Secret":
        """
        Finds a secret in the secret store by name and scope.
        """
        return cast(
            "Secret",
            await cls._static_request_async(
                "get",
                "/v1/apps/secrets/find",
                params=params,
            ),
        )

    @classmethod
    def list(
        cls, **params: Unpack["Secret.ListParams"]
    ) -> ListObject["Secret"]:
        """
        List all secrets stored on the given scope.
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
        cls, **params: Unpack["Secret.ListParams"]
    ) -> ListObject["Secret"]:
        """
        List all secrets stored on the given scope.
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

    _inner_class_types = {"scope": Scope}

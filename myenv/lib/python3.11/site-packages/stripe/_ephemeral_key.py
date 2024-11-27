# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._createable_api_resource import CreateableAPIResource
from stripe._deletable_api_resource import DeletableAPIResource
from stripe._request_options import RequestOptions
from stripe._util import class_method_variant, sanitize_id
from typing import ClassVar, List, Optional, cast, overload
from typing_extensions import Literal, NotRequired, Unpack


class EphemeralKey(
    CreateableAPIResource["EphemeralKey"],
    DeletableAPIResource["EphemeralKey"],
):
    OBJECT_NAME: ClassVar[Literal["ephemeral_key"]] = "ephemeral_key"

    class DeleteParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    created: int
    """
    Time at which the object was created. Measured in seconds since the Unix epoch.
    """
    expires: int
    """
    Time at which the key will expire. Measured in seconds since the Unix epoch.
    """
    id: str
    """
    Unique identifier for the object.
    """
    livemode: bool
    """
    Has the value `true` if the object exists in live mode or the value `false` if the object exists in test mode.
    """
    object: Literal["ephemeral_key"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """
    secret: Optional[str]
    """
    The key's secret. You can use this value to make authorized requests to the Stripe API.
    """

    @classmethod
    def _cls_delete(
        cls, sid: str, **params: Unpack["EphemeralKey.DeleteParams"]
    ) -> "EphemeralKey":
        """
        Invalidates a short-lived API key for a given resource.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(sid))
        return cast(
            "EphemeralKey",
            cls._static_request(
                "delete",
                url,
                params=params,
            ),
        )

    @overload
    @staticmethod
    def delete(
        sid: str, **params: Unpack["EphemeralKey.DeleteParams"]
    ) -> "EphemeralKey":
        """
        Invalidates a short-lived API key for a given resource.
        """
        ...

    @overload
    def delete(
        self, **params: Unpack["EphemeralKey.DeleteParams"]
    ) -> "EphemeralKey":
        """
        Invalidates a short-lived API key for a given resource.
        """
        ...

    @class_method_variant("_cls_delete")
    def delete(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["EphemeralKey.DeleteParams"]
    ) -> "EphemeralKey":
        """
        Invalidates a short-lived API key for a given resource.
        """
        return self._request_and_refresh(
            "delete",
            self.instance_url(),
            params=params,
        )

    @classmethod
    async def _cls_delete_async(
        cls, sid: str, **params: Unpack["EphemeralKey.DeleteParams"]
    ) -> "EphemeralKey":
        """
        Invalidates a short-lived API key for a given resource.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(sid))
        return cast(
            "EphemeralKey",
            await cls._static_request_async(
                "delete",
                url,
                params=params,
            ),
        )

    @overload
    @staticmethod
    async def delete_async(
        sid: str, **params: Unpack["EphemeralKey.DeleteParams"]
    ) -> "EphemeralKey":
        """
        Invalidates a short-lived API key for a given resource.
        """
        ...

    @overload
    async def delete_async(
        self, **params: Unpack["EphemeralKey.DeleteParams"]
    ) -> "EphemeralKey":
        """
        Invalidates a short-lived API key for a given resource.
        """
        ...

    @class_method_variant("_cls_delete_async")
    async def delete_async(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["EphemeralKey.DeleteParams"]
    ) -> "EphemeralKey":
        """
        Invalidates a short-lived API key for a given resource.
        """
        return await self._request_and_refresh_async(
            "delete",
            self.instance_url(),
            params=params,
        )

    @classmethod
    def create(cls, **params):
        if params.get("stripe_version") is None:
            raise ValueError(
                "stripe_version must be specified to create an ephemeral "
                "key"
            )

        url = cls.class_url()
        return cls._static_request(
            "post",
            url,
            params=params,
            base_address="api",
            api_mode="V1",
        )

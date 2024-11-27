# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._ephemeral_key import EphemeralKey
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from typing import List, cast
from typing_extensions import NotRequired, TypedDict


class EphemeralKeyService(StripeService):
    class CreateParams(TypedDict):
        customer: NotRequired[str]
        """
        The ID of the Customer you'd like to modify using the resulting ephemeral key.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        issuing_card: NotRequired[str]
        """
        The ID of the Issuing Card you'd like to access using the resulting ephemeral key.
        """
        nonce: NotRequired[str]
        """
        A single-use token, created by Stripe.js, used for creating ephemeral keys for Issuing Cards without exchanging sensitive information.
        """
        verification_session: NotRequired[str]
        """
        The ID of the Identity VerificationSession you'd like to access using the resulting ephemeral key
        """

    class DeleteParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    def delete(
        self,
        key: str,
        params: "EphemeralKeyService.DeleteParams" = {},
        options: RequestOptions = {},
    ) -> EphemeralKey:
        """
        Invalidates a short-lived API key for a given resource.
        """
        return cast(
            EphemeralKey,
            self._request(
                "delete",
                "/v1/ephemeral_keys/{key}".format(key=sanitize_id(key)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def delete_async(
        self,
        key: str,
        params: "EphemeralKeyService.DeleteParams" = {},
        options: RequestOptions = {},
    ) -> EphemeralKey:
        """
        Invalidates a short-lived API key for a given resource.
        """
        return cast(
            EphemeralKey,
            await self._request_async(
                "delete",
                "/v1/ephemeral_keys/{key}".format(key=sanitize_id(key)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self,
        params: "EphemeralKeyService.CreateParams" = {},
        options: RequestOptions = {},
    ) -> EphemeralKey:
        """
        Creates a short-lived API key for a given resource.
        """
        return cast(
            EphemeralKey,
            self._request(
                "post",
                "/v1/ephemeral_keys",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        params: "EphemeralKeyService.CreateParams" = {},
        options: RequestOptions = {},
    ) -> EphemeralKey:
        """
        Creates a short-lived API key for a given resource.
        """
        return cast(
            EphemeralKey,
            await self._request_async(
                "post",
                "/v1/ephemeral_keys",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

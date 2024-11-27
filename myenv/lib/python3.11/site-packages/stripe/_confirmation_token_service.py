# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._confirmation_token import ConfirmationToken
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from typing import List, cast
from typing_extensions import NotRequired, TypedDict


class ConfirmationTokenService(StripeService):
    class RetrieveParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    def retrieve(
        self,
        confirmation_token: str,
        params: "ConfirmationTokenService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> ConfirmationToken:
        """
        Retrieves an existing ConfirmationToken object
        """
        return cast(
            ConfirmationToken,
            self._request(
                "get",
                "/v1/confirmation_tokens/{confirmation_token}".format(
                    confirmation_token=sanitize_id(confirmation_token),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        confirmation_token: str,
        params: "ConfirmationTokenService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> ConfirmationToken:
        """
        Retrieves an existing ConfirmationToken object
        """
        return cast(
            ConfirmationToken,
            await self._request_async(
                "get",
                "/v1/confirmation_tokens/{confirmation_token}".format(
                    confirmation_token=sanitize_id(confirmation_token),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

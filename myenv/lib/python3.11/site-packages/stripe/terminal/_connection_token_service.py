# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe.terminal._connection_token import ConnectionToken
from typing import List, cast
from typing_extensions import NotRequired, TypedDict


class ConnectionTokenService(StripeService):
    class CreateParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        location: NotRequired[str]
        """
        The id of the location that this connection token is scoped to. If specified the connection token will only be usable with readers assigned to that location, otherwise the connection token will be usable with all readers. Note that location scoping only applies to internet-connected readers. For more details, see [the docs on scoping connection tokens](https://docs.stripe.com/terminal/fleet/locations-and-zones?dashboard-or-api=api#connection-tokens).
        """

    def create(
        self,
        params: "ConnectionTokenService.CreateParams" = {},
        options: RequestOptions = {},
    ) -> ConnectionToken:
        """
        To connect to a reader the Stripe Terminal SDK needs to retrieve a short-lived connection token from Stripe, proxied through your server. On your backend, add an endpoint that creates and returns a connection token.
        """
        return cast(
            ConnectionToken,
            self._request(
                "post",
                "/v1/terminal/connection_tokens",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        params: "ConnectionTokenService.CreateParams" = {},
        options: RequestOptions = {},
    ) -> ConnectionToken:
        """
        To connect to a reader the Stripe Terminal SDK needs to retrieve a short-lived connection token from Stripe, proxied through your server. On your backend, add an endpoint that creates and returns a connection token.
        """
        return cast(
            ConnectionToken,
            await self._request_async(
                "post",
                "/v1/terminal/connection_tokens",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

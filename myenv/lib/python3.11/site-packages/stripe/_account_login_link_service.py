# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._login_link import LoginLink
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from typing import List, cast
from typing_extensions import NotRequired, TypedDict


class AccountLoginLinkService(StripeService):
    class CreateParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    def create(
        self,
        account: str,
        params: "AccountLoginLinkService.CreateParams" = {},
        options: RequestOptions = {},
    ) -> LoginLink:
        """
        Creates a single-use login link for a connected account to access the Express Dashboard.

        You can only create login links for accounts that use the [Express Dashboard](https://stripe.com/connect/express-dashboard) and are connected to your platform.
        """
        return cast(
            LoginLink,
            self._request(
                "post",
                "/v1/accounts/{account}/login_links".format(
                    account=sanitize_id(account),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        account: str,
        params: "AccountLoginLinkService.CreateParams" = {},
        options: RequestOptions = {},
    ) -> LoginLink:
        """
        Creates a single-use login link for a connected account to access the Express Dashboard.

        You can only create login links for accounts that use the [Express Dashboard](https://stripe.com/connect/express-dashboard) and are connected to your platform.
        """
        return cast(
            LoginLink,
            await self._request_async(
                "post",
                "/v1/accounts/{account}/login_links".format(
                    account=sanitize_id(account),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

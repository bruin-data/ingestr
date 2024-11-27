# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from stripe.financial_connections._account_owner import AccountOwner
from typing import List, cast
from typing_extensions import NotRequired, TypedDict


class AccountOwnerService(StripeService):
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
        ownership: str
        """
        The ID of the ownership object to fetch owners from.
        """
        starting_after: NotRequired[str]
        """
        A cursor for use in pagination. `starting_after` is an object ID that defines your place in the list. For instance, if you make a list request and receive 100 objects, ending with `obj_foo`, your subsequent call can include `starting_after=obj_foo` in order to fetch the next page of the list.
        """

    def list(
        self,
        account: str,
        params: "AccountOwnerService.ListParams",
        options: RequestOptions = {},
    ) -> ListObject[AccountOwner]:
        """
        Lists all owners for a given Account
        """
        return cast(
            ListObject[AccountOwner],
            self._request(
                "get",
                "/v1/financial_connections/accounts/{account}/owners".format(
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
        params: "AccountOwnerService.ListParams",
        options: RequestOptions = {},
    ) -> ListObject[AccountOwner]:
        """
        Lists all owners for a given Account
        """
        return cast(
            ListObject[AccountOwner],
            await self._request_async(
                "get",
                "/v1/financial_connections/accounts/{account}/owners".format(
                    account=sanitize_id(account),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

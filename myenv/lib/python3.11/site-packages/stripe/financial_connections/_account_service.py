# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from stripe.financial_connections._account import Account
from stripe.financial_connections._account_owner_service import (
    AccountOwnerService,
)
from typing import List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class AccountService(StripeService):
    def __init__(self, requestor):
        super().__init__(requestor)
        self.owners = AccountOwnerService(self._requestor)

    class DisconnectParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class ListParams(TypedDict):
        account_holder: NotRequired["AccountService.ListParamsAccountHolder"]
        """
        If present, only return accounts that belong to the specified account holder. `account_holder[customer]` and `account_holder[account]` are mutually exclusive.
        """
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
        session: NotRequired[str]
        """
        If present, only return accounts that were collected as part of the given session.
        """
        starting_after: NotRequired[str]
        """
        A cursor for use in pagination. `starting_after` is an object ID that defines your place in the list. For instance, if you make a list request and receive 100 objects, ending with `obj_foo`, your subsequent call can include `starting_after=obj_foo` in order to fetch the next page of the list.
        """

    class ListParamsAccountHolder(TypedDict):
        account: NotRequired[str]
        """
        The ID of the Stripe account whose accounts will be retrieved.
        """
        customer: NotRequired[str]
        """
        The ID of the Stripe customer whose accounts will be retrieved.
        """

    class RefreshParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        features: List[Literal["balance", "ownership", "transactions"]]
        """
        The list of account features that you would like to refresh.
        """

    class RetrieveParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class SubscribeParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        features: List[Literal["transactions"]]
        """
        The list of account features to which you would like to subscribe.
        """

    class UnsubscribeParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        features: List[Literal["transactions"]]
        """
        The list of account features from which you would like to unsubscribe.
        """

    def list(
        self,
        params: "AccountService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Account]:
        """
        Returns a list of Financial Connections Account objects.
        """
        return cast(
            ListObject[Account],
            self._request(
                "get",
                "/v1/financial_connections/accounts",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "AccountService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Account]:
        """
        Returns a list of Financial Connections Account objects.
        """
        return cast(
            ListObject[Account],
            await self._request_async(
                "get",
                "/v1/financial_connections/accounts",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        account: str,
        params: "AccountService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Account:
        """
        Retrieves the details of an Financial Connections Account.
        """
        return cast(
            Account,
            self._request(
                "get",
                "/v1/financial_connections/accounts/{account}".format(
                    account=sanitize_id(account),
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
        params: "AccountService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Account:
        """
        Retrieves the details of an Financial Connections Account.
        """
        return cast(
            Account,
            await self._request_async(
                "get",
                "/v1/financial_connections/accounts/{account}".format(
                    account=sanitize_id(account),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def disconnect(
        self,
        account: str,
        params: "AccountService.DisconnectParams" = {},
        options: RequestOptions = {},
    ) -> Account:
        """
        Disables your access to a Financial Connections Account. You will no longer be able to access data associated with the account (e.g. balances, transactions).
        """
        return cast(
            Account,
            self._request(
                "post",
                "/v1/financial_connections/accounts/{account}/disconnect".format(
                    account=sanitize_id(account),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def disconnect_async(
        self,
        account: str,
        params: "AccountService.DisconnectParams" = {},
        options: RequestOptions = {},
    ) -> Account:
        """
        Disables your access to a Financial Connections Account. You will no longer be able to access data associated with the account (e.g. balances, transactions).
        """
        return cast(
            Account,
            await self._request_async(
                "post",
                "/v1/financial_connections/accounts/{account}/disconnect".format(
                    account=sanitize_id(account),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def refresh(
        self,
        account: str,
        params: "AccountService.RefreshParams",
        options: RequestOptions = {},
    ) -> Account:
        """
        Refreshes the data associated with a Financial Connections Account.
        """
        return cast(
            Account,
            self._request(
                "post",
                "/v1/financial_connections/accounts/{account}/refresh".format(
                    account=sanitize_id(account),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def refresh_async(
        self,
        account: str,
        params: "AccountService.RefreshParams",
        options: RequestOptions = {},
    ) -> Account:
        """
        Refreshes the data associated with a Financial Connections Account.
        """
        return cast(
            Account,
            await self._request_async(
                "post",
                "/v1/financial_connections/accounts/{account}/refresh".format(
                    account=sanitize_id(account),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def subscribe(
        self,
        account: str,
        params: "AccountService.SubscribeParams",
        options: RequestOptions = {},
    ) -> Account:
        """
        Subscribes to periodic refreshes of data associated with a Financial Connections Account.
        """
        return cast(
            Account,
            self._request(
                "post",
                "/v1/financial_connections/accounts/{account}/subscribe".format(
                    account=sanitize_id(account),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def subscribe_async(
        self,
        account: str,
        params: "AccountService.SubscribeParams",
        options: RequestOptions = {},
    ) -> Account:
        """
        Subscribes to periodic refreshes of data associated with a Financial Connections Account.
        """
        return cast(
            Account,
            await self._request_async(
                "post",
                "/v1/financial_connections/accounts/{account}/subscribe".format(
                    account=sanitize_id(account),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def unsubscribe(
        self,
        account: str,
        params: "AccountService.UnsubscribeParams",
        options: RequestOptions = {},
    ) -> Account:
        """
        Unsubscribes from periodic refreshes of data associated with a Financial Connections Account.
        """
        return cast(
            Account,
            self._request(
                "post",
                "/v1/financial_connections/accounts/{account}/unsubscribe".format(
                    account=sanitize_id(account),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def unsubscribe_async(
        self,
        account: str,
        params: "AccountService.UnsubscribeParams",
        options: RequestOptions = {},
    ) -> Account:
        """
        Unsubscribes from periodic refreshes of data associated with a Financial Connections Account.
        """
        return cast(
            Account,
            await self._request_async(
                "post",
                "/v1/financial_connections/accounts/{account}/unsubscribe".format(
                    account=sanitize_id(account),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

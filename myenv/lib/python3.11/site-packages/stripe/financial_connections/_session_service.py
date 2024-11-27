# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from stripe.financial_connections._session import Session
from typing import List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class SessionService(StripeService):
    class CreateParams(TypedDict):
        account_holder: "SessionService.CreateParamsAccountHolder"
        """
        The account holder to link accounts for.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        filters: NotRequired["SessionService.CreateParamsFilters"]
        """
        Filters to restrict the kinds of accounts to collect.
        """
        permissions: List[
            Literal["balances", "ownership", "payment_method", "transactions"]
        ]
        """
        List of data features that you would like to request access to.

        Possible values are `balances`, `transactions`, `ownership`, and `payment_method`.
        """
        prefetch: NotRequired[
            List[Literal["balances", "ownership", "transactions"]]
        ]
        """
        List of data features that you would like to retrieve upon account creation.
        """
        return_url: NotRequired[str]
        """
        For webview integrations only. Upon completing OAuth login in the native browser, the user will be redirected to this URL to return to your app.
        """

    class CreateParamsAccountHolder(TypedDict):
        account: NotRequired[str]
        """
        The ID of the Stripe account whose accounts will be retrieved. Should only be present if `type` is `account`.
        """
        customer: NotRequired[str]
        """
        The ID of the Stripe customer whose accounts will be retrieved. Should only be present if `type` is `customer`.
        """
        type: Literal["account", "customer"]
        """
        Type of account holder to collect accounts for.
        """

    class CreateParamsFilters(TypedDict):
        account_subcategories: NotRequired[
            List[
                Literal[
                    "checking",
                    "credit_card",
                    "line_of_credit",
                    "mortgage",
                    "savings",
                ]
            ]
        ]
        """
        Restricts the Session to subcategories of accounts that can be linked. Valid subcategories are: `checking`, `savings`, `mortgage`, `line_of_credit`, `credit_card`.
        """
        countries: NotRequired[List[str]]
        """
        List of countries from which to collect accounts.
        """

    class RetrieveParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    def retrieve(
        self,
        session: str,
        params: "SessionService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Session:
        """
        Retrieves the details of a Financial Connections Session
        """
        return cast(
            Session,
            self._request(
                "get",
                "/v1/financial_connections/sessions/{session}".format(
                    session=sanitize_id(session),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        session: str,
        params: "SessionService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Session:
        """
        Retrieves the details of a Financial Connections Session
        """
        return cast(
            Session,
            await self._request_async(
                "get",
                "/v1/financial_connections/sessions/{session}".format(
                    session=sanitize_id(session),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self,
        params: "SessionService.CreateParams",
        options: RequestOptions = {},
    ) -> Session:
        """
        To launch the Financial Connections authorization flow, create a Session. The session's client_secret can be used to launch the flow using Stripe.js.
        """
        return cast(
            Session,
            self._request(
                "post",
                "/v1/financial_connections/sessions",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        params: "SessionService.CreateParams",
        options: RequestOptions = {},
    ) -> Session:
        """
        To launch the Financial Connections authorization flow, create a Session. The session's client_secret can be used to launch the flow using Stripe.js.
        """
        return cast(
            Session,
            await self._request_async(
                "post",
                "/v1/financial_connections/sessions",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._createable_api_resource import CreateableAPIResource
from stripe._expandable_field import ExpandableField
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_object import StripeObject
from typing import ClassVar, List, Optional, cast
from typing_extensions import (
    Literal,
    NotRequired,
    TypedDict,
    Unpack,
    TYPE_CHECKING,
)

if TYPE_CHECKING:
    from stripe._account import Account as AccountResource
    from stripe._customer import Customer
    from stripe.financial_connections._account import (
        Account as FinancialConnectionsAccountResource,
    )


class Session(CreateableAPIResource["Session"]):
    """
    A Financial Connections Session is the secure way to programmatically launch the client-side Stripe.js modal that lets your users link their accounts.
    """

    OBJECT_NAME: ClassVar[Literal["financial_connections.session"]] = (
        "financial_connections.session"
    )

    class AccountHolder(StripeObject):
        account: Optional[ExpandableField["AccountResource"]]
        """
        The ID of the Stripe account this account belongs to. Should only be present if `account_holder.type` is `account`.
        """
        customer: Optional[ExpandableField["Customer"]]
        """
        ID of the Stripe customer this account belongs to. Present if and only if `account_holder.type` is `customer`.
        """
        type: Literal["account", "customer"]
        """
        Type of account holder that this account belongs to.
        """

    class Filters(StripeObject):
        account_subcategories: Optional[
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
        countries: Optional[List[str]]
        """
        List of countries from which to filter accounts.
        """

    class CreateParams(RequestOptions):
        account_holder: "Session.CreateParamsAccountHolder"
        """
        The account holder to link accounts for.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        filters: NotRequired["Session.CreateParamsFilters"]
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

    class RetrieveParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    account_holder: Optional[AccountHolder]
    """
    The account holder for whom accounts are collected in this session.
    """
    accounts: ListObject["FinancialConnectionsAccountResource"]
    """
    The accounts that were collected as part of this Session.
    """
    client_secret: str
    """
    A value that will be passed to the client to launch the authentication flow.
    """
    filters: Optional[Filters]
    id: str
    """
    Unique identifier for the object.
    """
    livemode: bool
    """
    Has the value `true` if the object exists in live mode or the value `false` if the object exists in test mode.
    """
    object: Literal["financial_connections.session"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """
    permissions: List[
        Literal["balances", "ownership", "payment_method", "transactions"]
    ]
    """
    Permissions requested for accounts collected during this session.
    """
    prefetch: Optional[List[Literal["balances", "ownership", "transactions"]]]
    """
    Data features requested to be retrieved upon account creation.
    """
    return_url: Optional[str]
    """
    For webview integrations only. Upon completing OAuth login in the native browser, the user will be redirected to this URL to return to your app.
    """

    @classmethod
    def create(cls, **params: Unpack["Session.CreateParams"]) -> "Session":
        """
        To launch the Financial Connections authorization flow, create a Session. The session's client_secret can be used to launch the flow using Stripe.js.
        """
        return cast(
            "Session",
            cls._static_request(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    async def create_async(
        cls, **params: Unpack["Session.CreateParams"]
    ) -> "Session":
        """
        To launch the Financial Connections authorization flow, create a Session. The session's client_secret can be used to launch the flow using Stripe.js.
        """
        return cast(
            "Session",
            await cls._static_request_async(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    def retrieve(
        cls, id: str, **params: Unpack["Session.RetrieveParams"]
    ) -> "Session":
        """
        Retrieves the details of a Financial Connections Session
        """
        instance = cls(id, **params)
        instance.refresh()
        return instance

    @classmethod
    async def retrieve_async(
        cls, id: str, **params: Unpack["Session.RetrieveParams"]
    ) -> "Session":
        """
        Retrieves the details of a Financial Connections Session
        """
        instance = cls(id, **params)
        await instance.refresh_async()
        return instance

    _inner_class_types = {"account_holder": AccountHolder, "filters": Filters}

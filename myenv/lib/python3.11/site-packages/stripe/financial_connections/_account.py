# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._expandable_field import ExpandableField
from stripe._list_object import ListObject
from stripe._listable_api_resource import ListableAPIResource
from stripe._request_options import RequestOptions
from stripe._stripe_object import StripeObject
from stripe._util import class_method_variant, sanitize_id
from typing import ClassVar, Dict, List, Optional, cast, overload
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
    from stripe.financial_connections._account_owner import AccountOwner
    from stripe.financial_connections._account_ownership import (
        AccountOwnership,
    )


class Account(ListableAPIResource["Account"]):
    """
    A Financial Connections Account represents an account that exists outside of Stripe, to which you have been granted some degree of access.
    """

    OBJECT_NAME: ClassVar[Literal["financial_connections.account"]] = (
        "financial_connections.account"
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

    class Balance(StripeObject):
        class Cash(StripeObject):
            available: Optional[Dict[str, int]]
            """
            The funds available to the account holder. Typically this is the current balance after subtracting any outbound pending transactions and adding any inbound pending transactions.

            Each key is a three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase.

            Each value is a integer amount. A positive amount indicates money owed to the account holder. A negative amount indicates money owed by the account holder.
            """

        class Credit(StripeObject):
            used: Optional[Dict[str, int]]
            """
            The credit that has been used by the account holder.

            Each key is a three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase.

            Each value is a integer amount. A positive amount indicates money owed to the account holder. A negative amount indicates money owed by the account holder.
            """

        as_of: int
        """
        The time that the external institution calculated this balance. Measured in seconds since the Unix epoch.
        """
        cash: Optional[Cash]
        credit: Optional[Credit]
        current: Dict[str, int]
        """
        The balances owed to (or by) the account holder, before subtracting any outbound pending transactions or adding any inbound pending transactions.

        Each key is a three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase.

        Each value is a integer amount. A positive amount indicates money owed to the account holder. A negative amount indicates money owed by the account holder.
        """
        type: Literal["cash", "credit"]
        """
        The `type` of the balance. An additional hash is included on the balance with a name matching this value.
        """
        _inner_class_types = {"cash": Cash, "credit": Credit}

    class BalanceRefresh(StripeObject):
        last_attempted_at: int
        """
        The time at which the last refresh attempt was initiated. Measured in seconds since the Unix epoch.
        """
        next_refresh_available_at: Optional[int]
        """
        Time at which the next balance refresh can be initiated. This value will be `null` when `status` is `pending`. Measured in seconds since the Unix epoch.
        """
        status: Literal["failed", "pending", "succeeded"]
        """
        The status of the last refresh attempt.
        """

    class OwnershipRefresh(StripeObject):
        last_attempted_at: int
        """
        The time at which the last refresh attempt was initiated. Measured in seconds since the Unix epoch.
        """
        next_refresh_available_at: Optional[int]
        """
        Time at which the next ownership refresh can be initiated. This value will be `null` when `status` is `pending`. Measured in seconds since the Unix epoch.
        """
        status: Literal["failed", "pending", "succeeded"]
        """
        The status of the last refresh attempt.
        """

    class TransactionRefresh(StripeObject):
        id: str
        """
        Unique identifier for the object.
        """
        last_attempted_at: int
        """
        The time at which the last refresh attempt was initiated. Measured in seconds since the Unix epoch.
        """
        next_refresh_available_at: Optional[int]
        """
        Time at which the next transaction refresh can be initiated. This value will be `null` when `status` is `pending`. Measured in seconds since the Unix epoch.
        """
        status: Literal["failed", "pending", "succeeded"]
        """
        The status of the last refresh attempt.
        """

    class DisconnectParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class ListOwnersParams(RequestOptions):
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

    class ListParams(RequestOptions):
        account_holder: NotRequired["Account.ListParamsAccountHolder"]
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

    class RefreshAccountParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        features: List[Literal["balance", "ownership", "transactions"]]
        """
        The list of account features that you would like to refresh.
        """

    class RetrieveParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class SubscribeParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        features: List[Literal["transactions"]]
        """
        The list of account features to which you would like to subscribe.
        """

    class UnsubscribeParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        features: List[Literal["transactions"]]
        """
        The list of account features from which you would like to unsubscribe.
        """

    account_holder: Optional[AccountHolder]
    """
    The account holder that this account belongs to.
    """
    balance: Optional[Balance]
    """
    The most recent information about the account's balance.
    """
    balance_refresh: Optional[BalanceRefresh]
    """
    The state of the most recent attempt to refresh the account balance.
    """
    category: Literal["cash", "credit", "investment", "other"]
    """
    The type of the account. Account category is further divided in `subcategory`.
    """
    created: int
    """
    Time at which the object was created. Measured in seconds since the Unix epoch.
    """
    display_name: Optional[str]
    """
    A human-readable name that has been assigned to this account, either by the account holder or by the institution.
    """
    id: str
    """
    Unique identifier for the object.
    """
    institution_name: str
    """
    The name of the institution that holds this account.
    """
    last4: Optional[str]
    """
    The last 4 digits of the account number. If present, this will be 4 numeric characters.
    """
    livemode: bool
    """
    Has the value `true` if the object exists in live mode or the value `false` if the object exists in test mode.
    """
    object: Literal["financial_connections.account"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """
    ownership: Optional[ExpandableField["AccountOwnership"]]
    """
    The most recent information about the account's owners.
    """
    ownership_refresh: Optional[OwnershipRefresh]
    """
    The state of the most recent attempt to refresh the account owners.
    """
    permissions: Optional[
        List[
            Literal["balances", "ownership", "payment_method", "transactions"]
        ]
    ]
    """
    The list of permissions granted by this account.
    """
    status: Literal["active", "disconnected", "inactive"]
    """
    The status of the link to the account.
    """
    subcategory: Literal[
        "checking",
        "credit_card",
        "line_of_credit",
        "mortgage",
        "other",
        "savings",
    ]
    """
    If `category` is `cash`, one of:

     - `checking`
     - `savings`
     - `other`

    If `category` is `credit`, one of:

     - `mortgage`
     - `line_of_credit`
     - `credit_card`
     - `other`

    If `category` is `investment` or `other`, this will be `other`.
    """
    subscriptions: Optional[List[Literal["transactions"]]]
    """
    The list of data refresh subscriptions requested on this account.
    """
    supported_payment_method_types: List[Literal["link", "us_bank_account"]]
    """
    The [PaymentMethod type](https://stripe.com/docs/api/payment_methods/object#payment_method_object-type)(s) that can be created from this account.
    """
    transaction_refresh: Optional[TransactionRefresh]
    """
    The state of the most recent attempt to refresh the account transactions.
    """

    @classmethod
    def _cls_disconnect(
        cls, account: str, **params: Unpack["Account.DisconnectParams"]
    ) -> "Account":
        """
        Disables your access to a Financial Connections Account. You will no longer be able to access data associated with the account (e.g. balances, transactions).
        """
        return cast(
            "Account",
            cls._static_request(
                "post",
                "/v1/financial_connections/accounts/{account}/disconnect".format(
                    account=sanitize_id(account)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    def disconnect(
        account: str, **params: Unpack["Account.DisconnectParams"]
    ) -> "Account":
        """
        Disables your access to a Financial Connections Account. You will no longer be able to access data associated with the account (e.g. balances, transactions).
        """
        ...

    @overload
    def disconnect(
        self, **params: Unpack["Account.DisconnectParams"]
    ) -> "Account":
        """
        Disables your access to a Financial Connections Account. You will no longer be able to access data associated with the account (e.g. balances, transactions).
        """
        ...

    @class_method_variant("_cls_disconnect")
    def disconnect(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Account.DisconnectParams"]
    ) -> "Account":
        """
        Disables your access to a Financial Connections Account. You will no longer be able to access data associated with the account (e.g. balances, transactions).
        """
        return cast(
            "Account",
            self._request(
                "post",
                "/v1/financial_connections/accounts/{account}/disconnect".format(
                    account=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    async def _cls_disconnect_async(
        cls, account: str, **params: Unpack["Account.DisconnectParams"]
    ) -> "Account":
        """
        Disables your access to a Financial Connections Account. You will no longer be able to access data associated with the account (e.g. balances, transactions).
        """
        return cast(
            "Account",
            await cls._static_request_async(
                "post",
                "/v1/financial_connections/accounts/{account}/disconnect".format(
                    account=sanitize_id(account)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    async def disconnect_async(
        account: str, **params: Unpack["Account.DisconnectParams"]
    ) -> "Account":
        """
        Disables your access to a Financial Connections Account. You will no longer be able to access data associated with the account (e.g. balances, transactions).
        """
        ...

    @overload
    async def disconnect_async(
        self, **params: Unpack["Account.DisconnectParams"]
    ) -> "Account":
        """
        Disables your access to a Financial Connections Account. You will no longer be able to access data associated with the account (e.g. balances, transactions).
        """
        ...

    @class_method_variant("_cls_disconnect_async")
    async def disconnect_async(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Account.DisconnectParams"]
    ) -> "Account":
        """
        Disables your access to a Financial Connections Account. You will no longer be able to access data associated with the account (e.g. balances, transactions).
        """
        return cast(
            "Account",
            await self._request_async(
                "post",
                "/v1/financial_connections/accounts/{account}/disconnect".format(
                    account=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    def list(
        cls, **params: Unpack["Account.ListParams"]
    ) -> ListObject["Account"]:
        """
        Returns a list of Financial Connections Account objects.
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
        cls, **params: Unpack["Account.ListParams"]
    ) -> ListObject["Account"]:
        """
        Returns a list of Financial Connections Account objects.
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

    @classmethod
    def _cls_list_owners(
        cls, account: str, **params: Unpack["Account.ListOwnersParams"]
    ) -> ListObject["AccountOwner"]:
        """
        Lists all owners for a given Account
        """
        return cast(
            ListObject["AccountOwner"],
            cls._static_request(
                "get",
                "/v1/financial_connections/accounts/{account}/owners".format(
                    account=sanitize_id(account)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    def list_owners(
        account: str, **params: Unpack["Account.ListOwnersParams"]
    ) -> ListObject["AccountOwner"]:
        """
        Lists all owners for a given Account
        """
        ...

    @overload
    def list_owners(
        self, **params: Unpack["Account.ListOwnersParams"]
    ) -> ListObject["AccountOwner"]:
        """
        Lists all owners for a given Account
        """
        ...

    @class_method_variant("_cls_list_owners")
    def list_owners(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Account.ListOwnersParams"]
    ) -> ListObject["AccountOwner"]:
        """
        Lists all owners for a given Account
        """
        return cast(
            ListObject["AccountOwner"],
            self._request(
                "get",
                "/v1/financial_connections/accounts/{account}/owners".format(
                    account=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    async def _cls_list_owners_async(
        cls, account: str, **params: Unpack["Account.ListOwnersParams"]
    ) -> ListObject["AccountOwner"]:
        """
        Lists all owners for a given Account
        """
        return cast(
            ListObject["AccountOwner"],
            await cls._static_request_async(
                "get",
                "/v1/financial_connections/accounts/{account}/owners".format(
                    account=sanitize_id(account)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    async def list_owners_async(
        account: str, **params: Unpack["Account.ListOwnersParams"]
    ) -> ListObject["AccountOwner"]:
        """
        Lists all owners for a given Account
        """
        ...

    @overload
    async def list_owners_async(
        self, **params: Unpack["Account.ListOwnersParams"]
    ) -> ListObject["AccountOwner"]:
        """
        Lists all owners for a given Account
        """
        ...

    @class_method_variant("_cls_list_owners_async")
    async def list_owners_async(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Account.ListOwnersParams"]
    ) -> ListObject["AccountOwner"]:
        """
        Lists all owners for a given Account
        """
        return cast(
            ListObject["AccountOwner"],
            await self._request_async(
                "get",
                "/v1/financial_connections/accounts/{account}/owners".format(
                    account=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    def _cls_refresh_account(
        cls, account: str, **params: Unpack["Account.RefreshAccountParams"]
    ) -> "Account":
        """
        Refreshes the data associated with a Financial Connections Account.
        """
        return cast(
            "Account",
            cls._static_request(
                "post",
                "/v1/financial_connections/accounts/{account}/refresh".format(
                    account=sanitize_id(account)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    def refresh_account(
        account: str, **params: Unpack["Account.RefreshAccountParams"]
    ) -> "Account":
        """
        Refreshes the data associated with a Financial Connections Account.
        """
        ...

    @overload
    def refresh_account(
        self, **params: Unpack["Account.RefreshAccountParams"]
    ) -> "Account":
        """
        Refreshes the data associated with a Financial Connections Account.
        """
        ...

    @class_method_variant("_cls_refresh_account")
    def refresh_account(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Account.RefreshAccountParams"]
    ) -> "Account":
        """
        Refreshes the data associated with a Financial Connections Account.
        """
        return cast(
            "Account",
            self._request(
                "post",
                "/v1/financial_connections/accounts/{account}/refresh".format(
                    account=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    async def _cls_refresh_account_async(
        cls, account: str, **params: Unpack["Account.RefreshAccountParams"]
    ) -> "Account":
        """
        Refreshes the data associated with a Financial Connections Account.
        """
        return cast(
            "Account",
            await cls._static_request_async(
                "post",
                "/v1/financial_connections/accounts/{account}/refresh".format(
                    account=sanitize_id(account)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    async def refresh_account_async(
        account: str, **params: Unpack["Account.RefreshAccountParams"]
    ) -> "Account":
        """
        Refreshes the data associated with a Financial Connections Account.
        """
        ...

    @overload
    async def refresh_account_async(
        self, **params: Unpack["Account.RefreshAccountParams"]
    ) -> "Account":
        """
        Refreshes the data associated with a Financial Connections Account.
        """
        ...

    @class_method_variant("_cls_refresh_account_async")
    async def refresh_account_async(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Account.RefreshAccountParams"]
    ) -> "Account":
        """
        Refreshes the data associated with a Financial Connections Account.
        """
        return cast(
            "Account",
            await self._request_async(
                "post",
                "/v1/financial_connections/accounts/{account}/refresh".format(
                    account=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    def retrieve(
        cls, id: str, **params: Unpack["Account.RetrieveParams"]
    ) -> "Account":
        """
        Retrieves the details of an Financial Connections Account.
        """
        instance = cls(id, **params)
        instance.refresh()
        return instance

    @classmethod
    async def retrieve_async(
        cls, id: str, **params: Unpack["Account.RetrieveParams"]
    ) -> "Account":
        """
        Retrieves the details of an Financial Connections Account.
        """
        instance = cls(id, **params)
        await instance.refresh_async()
        return instance

    @classmethod
    def _cls_subscribe(
        cls, account: str, **params: Unpack["Account.SubscribeParams"]
    ) -> "Account":
        """
        Subscribes to periodic refreshes of data associated with a Financial Connections Account.
        """
        return cast(
            "Account",
            cls._static_request(
                "post",
                "/v1/financial_connections/accounts/{account}/subscribe".format(
                    account=sanitize_id(account)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    def subscribe(
        account: str, **params: Unpack["Account.SubscribeParams"]
    ) -> "Account":
        """
        Subscribes to periodic refreshes of data associated with a Financial Connections Account.
        """
        ...

    @overload
    def subscribe(
        self, **params: Unpack["Account.SubscribeParams"]
    ) -> "Account":
        """
        Subscribes to periodic refreshes of data associated with a Financial Connections Account.
        """
        ...

    @class_method_variant("_cls_subscribe")
    def subscribe(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Account.SubscribeParams"]
    ) -> "Account":
        """
        Subscribes to periodic refreshes of data associated with a Financial Connections Account.
        """
        return cast(
            "Account",
            self._request(
                "post",
                "/v1/financial_connections/accounts/{account}/subscribe".format(
                    account=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    async def _cls_subscribe_async(
        cls, account: str, **params: Unpack["Account.SubscribeParams"]
    ) -> "Account":
        """
        Subscribes to periodic refreshes of data associated with a Financial Connections Account.
        """
        return cast(
            "Account",
            await cls._static_request_async(
                "post",
                "/v1/financial_connections/accounts/{account}/subscribe".format(
                    account=sanitize_id(account)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    async def subscribe_async(
        account: str, **params: Unpack["Account.SubscribeParams"]
    ) -> "Account":
        """
        Subscribes to periodic refreshes of data associated with a Financial Connections Account.
        """
        ...

    @overload
    async def subscribe_async(
        self, **params: Unpack["Account.SubscribeParams"]
    ) -> "Account":
        """
        Subscribes to periodic refreshes of data associated with a Financial Connections Account.
        """
        ...

    @class_method_variant("_cls_subscribe_async")
    async def subscribe_async(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Account.SubscribeParams"]
    ) -> "Account":
        """
        Subscribes to periodic refreshes of data associated with a Financial Connections Account.
        """
        return cast(
            "Account",
            await self._request_async(
                "post",
                "/v1/financial_connections/accounts/{account}/subscribe".format(
                    account=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    def _cls_unsubscribe(
        cls, account: str, **params: Unpack["Account.UnsubscribeParams"]
    ) -> "Account":
        """
        Unsubscribes from periodic refreshes of data associated with a Financial Connections Account.
        """
        return cast(
            "Account",
            cls._static_request(
                "post",
                "/v1/financial_connections/accounts/{account}/unsubscribe".format(
                    account=sanitize_id(account)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    def unsubscribe(
        account: str, **params: Unpack["Account.UnsubscribeParams"]
    ) -> "Account":
        """
        Unsubscribes from periodic refreshes of data associated with a Financial Connections Account.
        """
        ...

    @overload
    def unsubscribe(
        self, **params: Unpack["Account.UnsubscribeParams"]
    ) -> "Account":
        """
        Unsubscribes from periodic refreshes of data associated with a Financial Connections Account.
        """
        ...

    @class_method_variant("_cls_unsubscribe")
    def unsubscribe(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Account.UnsubscribeParams"]
    ) -> "Account":
        """
        Unsubscribes from periodic refreshes of data associated with a Financial Connections Account.
        """
        return cast(
            "Account",
            self._request(
                "post",
                "/v1/financial_connections/accounts/{account}/unsubscribe".format(
                    account=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    async def _cls_unsubscribe_async(
        cls, account: str, **params: Unpack["Account.UnsubscribeParams"]
    ) -> "Account":
        """
        Unsubscribes from periodic refreshes of data associated with a Financial Connections Account.
        """
        return cast(
            "Account",
            await cls._static_request_async(
                "post",
                "/v1/financial_connections/accounts/{account}/unsubscribe".format(
                    account=sanitize_id(account)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    async def unsubscribe_async(
        account: str, **params: Unpack["Account.UnsubscribeParams"]
    ) -> "Account":
        """
        Unsubscribes from periodic refreshes of data associated with a Financial Connections Account.
        """
        ...

    @overload
    async def unsubscribe_async(
        self, **params: Unpack["Account.UnsubscribeParams"]
    ) -> "Account":
        """
        Unsubscribes from periodic refreshes of data associated with a Financial Connections Account.
        """
        ...

    @class_method_variant("_cls_unsubscribe_async")
    async def unsubscribe_async(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Account.UnsubscribeParams"]
    ) -> "Account":
        """
        Unsubscribes from periodic refreshes of data associated with a Financial Connections Account.
        """
        return cast(
            "Account",
            await self._request_async(
                "post",
                "/v1/financial_connections/accounts/{account}/unsubscribe".format(
                    account=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    _inner_class_types = {
        "account_holder": AccountHolder,
        "balance": Balance,
        "balance_refresh": BalanceRefresh,
        "ownership_refresh": OwnershipRefresh,
        "transaction_refresh": TransactionRefresh,
    }

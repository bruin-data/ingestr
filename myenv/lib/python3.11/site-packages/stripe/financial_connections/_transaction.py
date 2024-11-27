# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._listable_api_resource import ListableAPIResource
from stripe._request_options import RequestOptions
from stripe._stripe_object import StripeObject
from typing import ClassVar, List, Optional
from typing_extensions import Literal, NotRequired, TypedDict, Unpack


class Transaction(ListableAPIResource["Transaction"]):
    """
    A Transaction represents a real transaction that affects a Financial Connections Account balance.
    """

    OBJECT_NAME: ClassVar[Literal["financial_connections.transaction"]] = (
        "financial_connections.transaction"
    )

    class StatusTransitions(StripeObject):
        posted_at: Optional[int]
        """
        Time at which this transaction posted. Measured in seconds since the Unix epoch.
        """
        void_at: Optional[int]
        """
        Time at which this transaction was voided. Measured in seconds since the Unix epoch.
        """

    class ListParams(RequestOptions):
        account: str
        """
        The ID of the Stripe account whose transactions will be retrieved.
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
        starting_after: NotRequired[str]
        """
        A cursor for use in pagination. `starting_after` is an object ID that defines your place in the list. For instance, if you make a list request and receive 100 objects, ending with `obj_foo`, your subsequent call can include `starting_after=obj_foo` in order to fetch the next page of the list.
        """
        transacted_at: NotRequired["Transaction.ListParamsTransactedAt|int"]
        """
        A filter on the list based on the object `transacted_at` field. The value can be a string with an integer Unix timestamp, or it can be a dictionary with the following options:
        """
        transaction_refresh: NotRequired[
            "Transaction.ListParamsTransactionRefresh"
        ]
        """
        A filter on the list based on the object `transaction_refresh` field. The value can be a dictionary with the following options:
        """

    class ListParamsTransactedAt(TypedDict):
        gt: NotRequired[int]
        """
        Minimum value to filter by (exclusive)
        """
        gte: NotRequired[int]
        """
        Minimum value to filter by (inclusive)
        """
        lt: NotRequired[int]
        """
        Maximum value to filter by (exclusive)
        """
        lte: NotRequired[int]
        """
        Maximum value to filter by (inclusive)
        """

    class ListParamsTransactionRefresh(TypedDict):
        after: str
        """
        Return results where the transactions were created or updated by a refresh that took place after this refresh (non-inclusive).
        """

    class RetrieveParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    account: str
    """
    The ID of the Financial Connections Account this transaction belongs to.
    """
    amount: int
    """
    The amount of this transaction, in cents (or local equivalent).
    """
    currency: str
    """
    Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
    """
    description: str
    """
    The description of this transaction.
    """
    id: str
    """
    Unique identifier for the object.
    """
    livemode: bool
    """
    Has the value `true` if the object exists in live mode or the value `false` if the object exists in test mode.
    """
    object: Literal["financial_connections.transaction"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """
    status: Literal["pending", "posted", "void"]
    """
    The status of the transaction.
    """
    status_transitions: StatusTransitions
    transacted_at: int
    """
    Time at which the transaction was transacted. Measured in seconds since the Unix epoch.
    """
    transaction_refresh: str
    """
    The token of the transaction refresh that last updated or created this transaction.
    """
    updated: int
    """
    Time at which the object was last updated. Measured in seconds since the Unix epoch.
    """

    @classmethod
    def list(
        cls, **params: Unpack["Transaction.ListParams"]
    ) -> ListObject["Transaction"]:
        """
        Returns a list of Financial Connections Transaction objects.
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
        cls, **params: Unpack["Transaction.ListParams"]
    ) -> ListObject["Transaction"]:
        """
        Returns a list of Financial Connections Transaction objects.
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
    def retrieve(
        cls, id: str, **params: Unpack["Transaction.RetrieveParams"]
    ) -> "Transaction":
        """
        Retrieves the details of a Financial Connections Transaction
        """
        instance = cls(id, **params)
        instance.refresh()
        return instance

    @classmethod
    async def retrieve_async(
        cls, id: str, **params: Unpack["Transaction.RetrieveParams"]
    ) -> "Transaction":
        """
        Retrieves the details of a Financial Connections Transaction
        """
        instance = cls(id, **params)
        await instance.refresh_async()
        return instance

    _inner_class_types = {"status_transitions": StatusTransitions}

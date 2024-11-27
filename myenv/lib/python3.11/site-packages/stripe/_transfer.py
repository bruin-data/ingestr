# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._createable_api_resource import CreateableAPIResource
from stripe._expandable_field import ExpandableField
from stripe._list_object import ListObject
from stripe._listable_api_resource import ListableAPIResource
from stripe._nested_resource_class_methods import nested_resource_class_methods
from stripe._request_options import RequestOptions
from stripe._updateable_api_resource import UpdateableAPIResource
from stripe._util import sanitize_id
from typing import ClassVar, Dict, List, Optional, cast
from typing_extensions import (
    Literal,
    NotRequired,
    TypedDict,
    Unpack,
    TYPE_CHECKING,
)

if TYPE_CHECKING:
    from stripe._account import Account
    from stripe._balance_transaction import BalanceTransaction
    from stripe._charge import Charge
    from stripe._reversal import Reversal


@nested_resource_class_methods("reversal")
class Transfer(
    CreateableAPIResource["Transfer"],
    ListableAPIResource["Transfer"],
    UpdateableAPIResource["Transfer"],
):
    """
    A `Transfer` object is created when you move funds between Stripe accounts as
    part of Connect.

    Before April 6, 2017, transfers also represented movement of funds from a
    Stripe account to a card or bank account. This behavior has since been split
    out into a [Payout](https://stripe.com/docs/api#payout_object) object, with corresponding payout endpoints. For more
    information, read about the
    [transfer/payout split](https://stripe.com/docs/transfer-payout-split).

    Related guide: [Creating separate charges and transfers](https://stripe.com/docs/connect/separate-charges-and-transfers)
    """

    OBJECT_NAME: ClassVar[Literal["transfer"]] = "transfer"

    class CreateParams(RequestOptions):
        amount: NotRequired[int]
        """
        A positive integer in cents (or local equivalent) representing how much to transfer.
        """
        currency: str
        """
        Three-letter [ISO code for currency](https://www.iso.org/iso-4217-currency-codes.html) in lowercase. Must be a [supported currency](https://docs.stripe.com/currencies).
        """
        description: NotRequired[str]
        """
        An arbitrary string attached to the object. Often useful for displaying to users.
        """
        destination: str
        """
        The ID of a connected Stripe account. [See the Connect documentation](https://stripe.com/docs/connect/separate-charges-and-transfers) for details.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        source_transaction: NotRequired[str]
        """
        You can use this parameter to transfer funds from a charge before they are added to your available balance. A pending balance will transfer immediately but the funds will not become available until the original charge becomes available. [See the Connect documentation](https://stripe.com/docs/connect/separate-charges-and-transfers#transfer-availability) for details.
        """
        source_type: NotRequired[Literal["bank_account", "card", "fpx"]]
        """
        The source balance to use for this transfer. One of `bank_account`, `card`, or `fpx`. For most users, this will default to `card`.
        """
        transfer_group: NotRequired[str]
        """
        A string that identifies this transaction as part of a group. See the [Connect documentation](https://stripe.com/docs/connect/separate-charges-and-transfers#transfer-options) for details.
        """

    class CreateReversalParams(RequestOptions):
        amount: NotRequired[int]
        """
        A positive integer in cents (or local equivalent) representing how much of this transfer to reverse. Can only reverse up to the unreversed amount remaining of the transfer. Partial transfer reversals are only allowed for transfers to Stripe Accounts. Defaults to the entire transfer amount.
        """
        description: NotRequired[str]
        """
        An arbitrary string which you can attach to a reversal object. This will be unset if you POST an empty value.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        refund_application_fee: NotRequired[bool]
        """
        Boolean indicating whether the application fee should be refunded when reversing this transfer. If a full transfer reversal is given, the full application fee will be refunded. Otherwise, the application fee will be refunded with an amount proportional to the amount of the transfer reversed.
        """

    class ListParams(RequestOptions):
        created: NotRequired["Transfer.ListParamsCreated|int"]
        """
        Only return transfers that were created during the given date interval.
        """
        destination: NotRequired[str]
        """
        Only return transfers for the destination specified by this account ID.
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
        transfer_group: NotRequired[str]
        """
        Only return transfers with the specified transfer group.
        """

    class ListParamsCreated(TypedDict):
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

    class ListReversalsParams(RequestOptions):
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

    class ModifyParams(RequestOptions):
        description: NotRequired[str]
        """
        An arbitrary string attached to the object. Often useful for displaying to users.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """

    class ModifyReversalParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """

    class RetrieveParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class RetrieveReversalParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    amount: int
    """
    Amount in cents (or local equivalent) to be transferred.
    """
    amount_reversed: int
    """
    Amount in cents (or local equivalent) reversed (can be less than the amount attribute on the transfer if a partial reversal was issued).
    """
    balance_transaction: Optional[ExpandableField["BalanceTransaction"]]
    """
    Balance transaction that describes the impact of this transfer on your account balance.
    """
    created: int
    """
    Time that this record of the transfer was first created.
    """
    currency: str
    """
    Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
    """
    description: Optional[str]
    """
    An arbitrary string attached to the object. Often useful for displaying to users.
    """
    destination: Optional[ExpandableField["Account"]]
    """
    ID of the Stripe account the transfer was sent to.
    """
    destination_payment: Optional[ExpandableField["Charge"]]
    """
    If the destination is a Stripe account, this will be the ID of the payment that the destination account received for the transfer.
    """
    id: str
    """
    Unique identifier for the object.
    """
    livemode: bool
    """
    Has the value `true` if the object exists in live mode or the value `false` if the object exists in test mode.
    """
    metadata: Dict[str, str]
    """
    Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format.
    """
    object: Literal["transfer"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """
    reversals: ListObject["Reversal"]
    """
    A list of reversals that have been applied to the transfer.
    """
    reversed: bool
    """
    Whether the transfer has been fully reversed. If the transfer is only partially reversed, this attribute will still be false.
    """
    source_transaction: Optional[ExpandableField["Charge"]]
    """
    ID of the charge or payment that was used to fund the transfer. If null, the transfer was funded from the available balance.
    """
    source_type: Optional[str]
    """
    The source balance this transfer came from. One of `card`, `fpx`, or `bank_account`.
    """
    transfer_group: Optional[str]
    """
    A string that identifies this transaction as part of a group. See the [Connect documentation](https://stripe.com/docs/connect/separate-charges-and-transfers#transfer-options) for details.
    """

    @classmethod
    def create(cls, **params: Unpack["Transfer.CreateParams"]) -> "Transfer":
        """
        To send funds from your Stripe account to a connected account, you create a new transfer object. Your [Stripe balance](https://stripe.com/docs/api#balance) must be able to cover the transfer amount, or you'll receive an “Insufficient Funds” error.
        """
        return cast(
            "Transfer",
            cls._static_request(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    async def create_async(
        cls, **params: Unpack["Transfer.CreateParams"]
    ) -> "Transfer":
        """
        To send funds from your Stripe account to a connected account, you create a new transfer object. Your [Stripe balance](https://stripe.com/docs/api#balance) must be able to cover the transfer amount, or you'll receive an “Insufficient Funds” error.
        """
        return cast(
            "Transfer",
            await cls._static_request_async(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    def list(
        cls, **params: Unpack["Transfer.ListParams"]
    ) -> ListObject["Transfer"]:
        """
        Returns a list of existing transfers sent to connected accounts. The transfers are returned in sorted order, with the most recently created transfers appearing first.
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
        cls, **params: Unpack["Transfer.ListParams"]
    ) -> ListObject["Transfer"]:
        """
        Returns a list of existing transfers sent to connected accounts. The transfers are returned in sorted order, with the most recently created transfers appearing first.
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
    def modify(
        cls, id: str, **params: Unpack["Transfer.ModifyParams"]
    ) -> "Transfer":
        """
        Updates the specified transfer by setting the values of the parameters passed. Any parameters not provided will be left unchanged.

        This request accepts only metadata as an argument.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(id))
        return cast(
            "Transfer",
            cls._static_request(
                "post",
                url,
                params=params,
            ),
        )

    @classmethod
    async def modify_async(
        cls, id: str, **params: Unpack["Transfer.ModifyParams"]
    ) -> "Transfer":
        """
        Updates the specified transfer by setting the values of the parameters passed. Any parameters not provided will be left unchanged.

        This request accepts only metadata as an argument.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(id))
        return cast(
            "Transfer",
            await cls._static_request_async(
                "post",
                url,
                params=params,
            ),
        )

    @classmethod
    def retrieve(
        cls, id: str, **params: Unpack["Transfer.RetrieveParams"]
    ) -> "Transfer":
        """
        Retrieves the details of an existing transfer. Supply the unique transfer ID from either a transfer creation request or the transfer list, and Stripe will return the corresponding transfer information.
        """
        instance = cls(id, **params)
        instance.refresh()
        return instance

    @classmethod
    async def retrieve_async(
        cls, id: str, **params: Unpack["Transfer.RetrieveParams"]
    ) -> "Transfer":
        """
        Retrieves the details of an existing transfer. Supply the unique transfer ID from either a transfer creation request or the transfer list, and Stripe will return the corresponding transfer information.
        """
        instance = cls(id, **params)
        await instance.refresh_async()
        return instance

    @classmethod
    def create_reversal(
        cls, id: str, **params: Unpack["Transfer.CreateReversalParams"]
    ) -> "Reversal":
        """
        When you create a new reversal, you must specify a transfer to create it on.

        When reversing transfers, you can optionally reverse part of the transfer. You can do so as many times as you wish until the entire transfer has been reversed.

        Once entirely reversed, a transfer can't be reversed again. This method will return an error when called on an already-reversed transfer, or when trying to reverse more money than is left on a transfer.
        """
        return cast(
            "Reversal",
            cls._static_request(
                "post",
                "/v1/transfers/{id}/reversals".format(id=sanitize_id(id)),
                params=params,
            ),
        )

    @classmethod
    async def create_reversal_async(
        cls, id: str, **params: Unpack["Transfer.CreateReversalParams"]
    ) -> "Reversal":
        """
        When you create a new reversal, you must specify a transfer to create it on.

        When reversing transfers, you can optionally reverse part of the transfer. You can do so as many times as you wish until the entire transfer has been reversed.

        Once entirely reversed, a transfer can't be reversed again. This method will return an error when called on an already-reversed transfer, or when trying to reverse more money than is left on a transfer.
        """
        return cast(
            "Reversal",
            await cls._static_request_async(
                "post",
                "/v1/transfers/{id}/reversals".format(id=sanitize_id(id)),
                params=params,
            ),
        )

    @classmethod
    def retrieve_reversal(
        cls,
        transfer: str,
        id: str,
        **params: Unpack["Transfer.RetrieveReversalParams"],
    ) -> "Reversal":
        """
        By default, you can see the 10 most recent reversals stored directly on the transfer object, but you can also retrieve details about a specific reversal stored on the transfer.
        """
        return cast(
            "Reversal",
            cls._static_request(
                "get",
                "/v1/transfers/{transfer}/reversals/{id}".format(
                    transfer=sanitize_id(transfer), id=sanitize_id(id)
                ),
                params=params,
            ),
        )

    @classmethod
    async def retrieve_reversal_async(
        cls,
        transfer: str,
        id: str,
        **params: Unpack["Transfer.RetrieveReversalParams"],
    ) -> "Reversal":
        """
        By default, you can see the 10 most recent reversals stored directly on the transfer object, but you can also retrieve details about a specific reversal stored on the transfer.
        """
        return cast(
            "Reversal",
            await cls._static_request_async(
                "get",
                "/v1/transfers/{transfer}/reversals/{id}".format(
                    transfer=sanitize_id(transfer), id=sanitize_id(id)
                ),
                params=params,
            ),
        )

    @classmethod
    def modify_reversal(
        cls,
        transfer: str,
        id: str,
        **params: Unpack["Transfer.ModifyReversalParams"],
    ) -> "Reversal":
        """
        Updates the specified reversal by setting the values of the parameters passed. Any parameters not provided will be left unchanged.

        This request only accepts metadata and description as arguments.
        """
        return cast(
            "Reversal",
            cls._static_request(
                "post",
                "/v1/transfers/{transfer}/reversals/{id}".format(
                    transfer=sanitize_id(transfer), id=sanitize_id(id)
                ),
                params=params,
            ),
        )

    @classmethod
    async def modify_reversal_async(
        cls,
        transfer: str,
        id: str,
        **params: Unpack["Transfer.ModifyReversalParams"],
    ) -> "Reversal":
        """
        Updates the specified reversal by setting the values of the parameters passed. Any parameters not provided will be left unchanged.

        This request only accepts metadata and description as arguments.
        """
        return cast(
            "Reversal",
            await cls._static_request_async(
                "post",
                "/v1/transfers/{transfer}/reversals/{id}".format(
                    transfer=sanitize_id(transfer), id=sanitize_id(id)
                ),
                params=params,
            ),
        )

    @classmethod
    def list_reversals(
        cls, id: str, **params: Unpack["Transfer.ListReversalsParams"]
    ) -> ListObject["Reversal"]:
        """
        You can see a list of the reversals belonging to a specific transfer. Note that the 10 most recent reversals are always available by default on the transfer object. If you need more than those 10, you can use this API method and the limit and starting_after parameters to page through additional reversals.
        """
        return cast(
            ListObject["Reversal"],
            cls._static_request(
                "get",
                "/v1/transfers/{id}/reversals".format(id=sanitize_id(id)),
                params=params,
            ),
        )

    @classmethod
    async def list_reversals_async(
        cls, id: str, **params: Unpack["Transfer.ListReversalsParams"]
    ) -> ListObject["Reversal"]:
        """
        You can see a list of the reversals belonging to a specific transfer. Note that the 10 most recent reversals are always available by default on the transfer object. If you need more than those 10, you can use this API method and the limit and starting_after parameters to page through additional reversals.
        """
        return cast(
            ListObject["Reversal"],
            await cls._static_request_async(
                "get",
                "/v1/transfers/{id}/reversals".format(id=sanitize_id(id)),
                params=params,
            ),
        )

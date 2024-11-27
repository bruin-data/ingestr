# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._expandable_field import ExpandableField
from stripe._list_object import ListObject
from stripe._listable_api_resource import ListableAPIResource
from stripe._nested_resource_class_methods import nested_resource_class_methods
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
    from stripe._account import Account
    from stripe._application import Application
    from stripe._application_fee_refund import ApplicationFeeRefund
    from stripe._balance_transaction import BalanceTransaction
    from stripe._charge import Charge


@nested_resource_class_methods("refund")
class ApplicationFee(ListableAPIResource["ApplicationFee"]):
    OBJECT_NAME: ClassVar[Literal["application_fee"]] = "application_fee"

    class FeeSource(StripeObject):
        charge: Optional[str]
        """
        Charge ID that created this application fee.
        """
        payout: Optional[str]
        """
        Payout ID that created this application fee.
        """
        type: Literal["charge", "payout"]
        """
        Type of object that created the application fee, either `charge` or `payout`.
        """

    class CreateRefundParams(RequestOptions):
        amount: NotRequired[int]
        """
        A positive integer, in _cents (or local equivalent)_, representing how much of this fee to refund. Can refund only up to the remaining unrefunded amount of the fee.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """

    class ListParams(RequestOptions):
        charge: NotRequired[str]
        """
        Only return application fees for the charge specified by this charge ID.
        """
        created: NotRequired["ApplicationFee.ListParamsCreated|int"]
        """
        Only return applications fees that were created during the given date interval.
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

    class ListRefundsParams(RequestOptions):
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

    class ModifyRefundParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """

    class RefundParams(RequestOptions):
        amount: NotRequired[int]
        """
        A positive integer, in _cents (or local equivalent)_, representing how much of this fee to refund. Can refund only up to the remaining unrefunded amount of the fee.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """

    class RetrieveParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class RetrieveRefundParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    account: ExpandableField["Account"]
    """
    ID of the Stripe account this fee was taken from.
    """
    amount: int
    """
    Amount earned, in cents (or local equivalent).
    """
    amount_refunded: int
    """
    Amount in cents (or local equivalent) refunded (can be less than the amount attribute on the fee if a partial refund was issued)
    """
    application: ExpandableField["Application"]
    """
    ID of the Connect application that earned the fee.
    """
    balance_transaction: Optional[ExpandableField["BalanceTransaction"]]
    """
    Balance transaction that describes the impact of this collected application fee on your account balance (not including refunds).
    """
    charge: ExpandableField["Charge"]
    """
    ID of the charge that the application fee was taken from.
    """
    created: int
    """
    Time at which the object was created. Measured in seconds since the Unix epoch.
    """
    currency: str
    """
    Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
    """
    fee_source: Optional[FeeSource]
    """
    Polymorphic source of the application fee. Includes the ID of the object the application fee was created from.
    """
    id: str
    """
    Unique identifier for the object.
    """
    livemode: bool
    """
    Has the value `true` if the object exists in live mode or the value `false` if the object exists in test mode.
    """
    object: Literal["application_fee"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """
    originating_transaction: Optional[ExpandableField["Charge"]]
    """
    ID of the corresponding charge on the platform account, if this fee was the result of a charge using the `destination` parameter.
    """
    refunded: bool
    """
    Whether the fee has been fully refunded. If the fee is only partially refunded, this attribute will still be false.
    """
    refunds: ListObject["ApplicationFeeRefund"]
    """
    A list of refunds that have been applied to the fee.
    """

    @classmethod
    def list(
        cls, **params: Unpack["ApplicationFee.ListParams"]
    ) -> ListObject["ApplicationFee"]:
        """
        Returns a list of application fees you've previously collected. The application fees are returned in sorted order, with the most recent fees appearing first.
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
        cls, **params: Unpack["ApplicationFee.ListParams"]
    ) -> ListObject["ApplicationFee"]:
        """
        Returns a list of application fees you've previously collected. The application fees are returned in sorted order, with the most recent fees appearing first.
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
    def _cls_refund(
        cls, id: str, **params: Unpack["ApplicationFee.RefundParams"]
    ) -> "ApplicationFeeRefund":
        """
        Refunds an application fee that has previously been collected but not yet refunded.
        Funds will be refunded to the Stripe account from which the fee was originally collected.

        You can optionally refund only part of an application fee.
        You can do so multiple times, until the entire fee has been refunded.

        Once entirely refunded, an application fee can't be refunded again.
        This method will raise an error when called on an already-refunded application fee,
        or when trying to refund more money than is left on an application fee.
        """
        return cast(
            "ApplicationFeeRefund",
            cls._static_request(
                "post",
                "/v1/application_fees/{id}/refunds".format(id=sanitize_id(id)),
                params=params,
            ),
        )

    @overload
    @staticmethod
    def refund(
        id: str, **params: Unpack["ApplicationFee.RefundParams"]
    ) -> "ApplicationFeeRefund":
        """
        Refunds an application fee that has previously been collected but not yet refunded.
        Funds will be refunded to the Stripe account from which the fee was originally collected.

        You can optionally refund only part of an application fee.
        You can do so multiple times, until the entire fee has been refunded.

        Once entirely refunded, an application fee can't be refunded again.
        This method will raise an error when called on an already-refunded application fee,
        or when trying to refund more money than is left on an application fee.
        """
        ...

    @overload
    def refund(
        self, **params: Unpack["ApplicationFee.RefundParams"]
    ) -> "ApplicationFeeRefund":
        """
        Refunds an application fee that has previously been collected but not yet refunded.
        Funds will be refunded to the Stripe account from which the fee was originally collected.

        You can optionally refund only part of an application fee.
        You can do so multiple times, until the entire fee has been refunded.

        Once entirely refunded, an application fee can't be refunded again.
        This method will raise an error when called on an already-refunded application fee,
        or when trying to refund more money than is left on an application fee.
        """
        ...

    @class_method_variant("_cls_refund")
    def refund(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["ApplicationFee.RefundParams"]
    ) -> "ApplicationFeeRefund":
        """
        Refunds an application fee that has previously been collected but not yet refunded.
        Funds will be refunded to the Stripe account from which the fee was originally collected.

        You can optionally refund only part of an application fee.
        You can do so multiple times, until the entire fee has been refunded.

        Once entirely refunded, an application fee can't be refunded again.
        This method will raise an error when called on an already-refunded application fee,
        or when trying to refund more money than is left on an application fee.
        """
        return cast(
            "ApplicationFeeRefund",
            self._request(
                "post",
                "/v1/application_fees/{id}/refunds".format(
                    id=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    async def _cls_refund_async(
        cls, id: str, **params: Unpack["ApplicationFee.RefundParams"]
    ) -> "ApplicationFeeRefund":
        """
        Refunds an application fee that has previously been collected but not yet refunded.
        Funds will be refunded to the Stripe account from which the fee was originally collected.

        You can optionally refund only part of an application fee.
        You can do so multiple times, until the entire fee has been refunded.

        Once entirely refunded, an application fee can't be refunded again.
        This method will raise an error when called on an already-refunded application fee,
        or when trying to refund more money than is left on an application fee.
        """
        return cast(
            "ApplicationFeeRefund",
            await cls._static_request_async(
                "post",
                "/v1/application_fees/{id}/refunds".format(id=sanitize_id(id)),
                params=params,
            ),
        )

    @overload
    @staticmethod
    async def refund_async(
        id: str, **params: Unpack["ApplicationFee.RefundParams"]
    ) -> "ApplicationFeeRefund":
        """
        Refunds an application fee that has previously been collected but not yet refunded.
        Funds will be refunded to the Stripe account from which the fee was originally collected.

        You can optionally refund only part of an application fee.
        You can do so multiple times, until the entire fee has been refunded.

        Once entirely refunded, an application fee can't be refunded again.
        This method will raise an error when called on an already-refunded application fee,
        or when trying to refund more money than is left on an application fee.
        """
        ...

    @overload
    async def refund_async(
        self, **params: Unpack["ApplicationFee.RefundParams"]
    ) -> "ApplicationFeeRefund":
        """
        Refunds an application fee that has previously been collected but not yet refunded.
        Funds will be refunded to the Stripe account from which the fee was originally collected.

        You can optionally refund only part of an application fee.
        You can do so multiple times, until the entire fee has been refunded.

        Once entirely refunded, an application fee can't be refunded again.
        This method will raise an error when called on an already-refunded application fee,
        or when trying to refund more money than is left on an application fee.
        """
        ...

    @class_method_variant("_cls_refund_async")
    async def refund_async(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["ApplicationFee.RefundParams"]
    ) -> "ApplicationFeeRefund":
        """
        Refunds an application fee that has previously been collected but not yet refunded.
        Funds will be refunded to the Stripe account from which the fee was originally collected.

        You can optionally refund only part of an application fee.
        You can do so multiple times, until the entire fee has been refunded.

        Once entirely refunded, an application fee can't be refunded again.
        This method will raise an error when called on an already-refunded application fee,
        or when trying to refund more money than is left on an application fee.
        """
        return cast(
            "ApplicationFeeRefund",
            await self._request_async(
                "post",
                "/v1/application_fees/{id}/refunds".format(
                    id=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    def retrieve(
        cls, id: str, **params: Unpack["ApplicationFee.RetrieveParams"]
    ) -> "ApplicationFee":
        """
        Retrieves the details of an application fee that your account has collected. The same information is returned when refunding the application fee.
        """
        instance = cls(id, **params)
        instance.refresh()
        return instance

    @classmethod
    async def retrieve_async(
        cls, id: str, **params: Unpack["ApplicationFee.RetrieveParams"]
    ) -> "ApplicationFee":
        """
        Retrieves the details of an application fee that your account has collected. The same information is returned when refunding the application fee.
        """
        instance = cls(id, **params)
        await instance.refresh_async()
        return instance

    @classmethod
    def create_refund(
        cls, id: str, **params: Unpack["ApplicationFee.CreateRefundParams"]
    ) -> "ApplicationFeeRefund":
        """
        Refunds an application fee that has previously been collected but not yet refunded.
        Funds will be refunded to the Stripe account from which the fee was originally collected.

        You can optionally refund only part of an application fee.
        You can do so multiple times, until the entire fee has been refunded.

        Once entirely refunded, an application fee can't be refunded again.
        This method will raise an error when called on an already-refunded application fee,
        or when trying to refund more money than is left on an application fee.
        """
        return cast(
            "ApplicationFeeRefund",
            cls._static_request(
                "post",
                "/v1/application_fees/{id}/refunds".format(id=sanitize_id(id)),
                params=params,
            ),
        )

    @classmethod
    async def create_refund_async(
        cls, id: str, **params: Unpack["ApplicationFee.CreateRefundParams"]
    ) -> "ApplicationFeeRefund":
        """
        Refunds an application fee that has previously been collected but not yet refunded.
        Funds will be refunded to the Stripe account from which the fee was originally collected.

        You can optionally refund only part of an application fee.
        You can do so multiple times, until the entire fee has been refunded.

        Once entirely refunded, an application fee can't be refunded again.
        This method will raise an error when called on an already-refunded application fee,
        or when trying to refund more money than is left on an application fee.
        """
        return cast(
            "ApplicationFeeRefund",
            await cls._static_request_async(
                "post",
                "/v1/application_fees/{id}/refunds".format(id=sanitize_id(id)),
                params=params,
            ),
        )

    @classmethod
    def retrieve_refund(
        cls,
        fee: str,
        id: str,
        **params: Unpack["ApplicationFee.RetrieveRefundParams"],
    ) -> "ApplicationFeeRefund":
        """
        By default, you can see the 10 most recent refunds stored directly on the application fee object, but you can also retrieve details about a specific refund stored on the application fee.
        """
        return cast(
            "ApplicationFeeRefund",
            cls._static_request(
                "get",
                "/v1/application_fees/{fee}/refunds/{id}".format(
                    fee=sanitize_id(fee), id=sanitize_id(id)
                ),
                params=params,
            ),
        )

    @classmethod
    async def retrieve_refund_async(
        cls,
        fee: str,
        id: str,
        **params: Unpack["ApplicationFee.RetrieveRefundParams"],
    ) -> "ApplicationFeeRefund":
        """
        By default, you can see the 10 most recent refunds stored directly on the application fee object, but you can also retrieve details about a specific refund stored on the application fee.
        """
        return cast(
            "ApplicationFeeRefund",
            await cls._static_request_async(
                "get",
                "/v1/application_fees/{fee}/refunds/{id}".format(
                    fee=sanitize_id(fee), id=sanitize_id(id)
                ),
                params=params,
            ),
        )

    @classmethod
    def modify_refund(
        cls,
        fee: str,
        id: str,
        **params: Unpack["ApplicationFee.ModifyRefundParams"],
    ) -> "ApplicationFeeRefund":
        """
        Updates the specified application fee refund by setting the values of the parameters passed. Any parameters not provided will be left unchanged.

        This request only accepts metadata as an argument.
        """
        return cast(
            "ApplicationFeeRefund",
            cls._static_request(
                "post",
                "/v1/application_fees/{fee}/refunds/{id}".format(
                    fee=sanitize_id(fee), id=sanitize_id(id)
                ),
                params=params,
            ),
        )

    @classmethod
    async def modify_refund_async(
        cls,
        fee: str,
        id: str,
        **params: Unpack["ApplicationFee.ModifyRefundParams"],
    ) -> "ApplicationFeeRefund":
        """
        Updates the specified application fee refund by setting the values of the parameters passed. Any parameters not provided will be left unchanged.

        This request only accepts metadata as an argument.
        """
        return cast(
            "ApplicationFeeRefund",
            await cls._static_request_async(
                "post",
                "/v1/application_fees/{fee}/refunds/{id}".format(
                    fee=sanitize_id(fee), id=sanitize_id(id)
                ),
                params=params,
            ),
        )

    @classmethod
    def list_refunds(
        cls, id: str, **params: Unpack["ApplicationFee.ListRefundsParams"]
    ) -> ListObject["ApplicationFeeRefund"]:
        """
        You can see a list of the refunds belonging to a specific application fee. Note that the 10 most recent refunds are always available by default on the application fee object. If you need more than those 10, you can use this API method and the limit and starting_after parameters to page through additional refunds.
        """
        return cast(
            ListObject["ApplicationFeeRefund"],
            cls._static_request(
                "get",
                "/v1/application_fees/{id}/refunds".format(id=sanitize_id(id)),
                params=params,
            ),
        )

    @classmethod
    async def list_refunds_async(
        cls, id: str, **params: Unpack["ApplicationFee.ListRefundsParams"]
    ) -> ListObject["ApplicationFeeRefund"]:
        """
        You can see a list of the refunds belonging to a specific application fee. Note that the 10 most recent refunds are always available by default on the application fee object. If you need more than those 10, you can use this API method and the limit and starting_after parameters to page through additional refunds.
        """
        return cast(
            ListObject["ApplicationFeeRefund"],
            await cls._static_request_async(
                "get",
                "/v1/application_fees/{id}/refunds".format(id=sanitize_id(id)),
                params=params,
            ),
        )

    _inner_class_types = {"fee_source": FeeSource}

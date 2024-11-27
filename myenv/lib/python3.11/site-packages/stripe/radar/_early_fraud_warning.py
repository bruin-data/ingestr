# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._expandable_field import ExpandableField
from stripe._list_object import ListObject
from stripe._listable_api_resource import ListableAPIResource
from stripe._request_options import RequestOptions
from typing import ClassVar, List, Optional
from typing_extensions import (
    Literal,
    NotRequired,
    TypedDict,
    Unpack,
    TYPE_CHECKING,
)

if TYPE_CHECKING:
    from stripe._charge import Charge
    from stripe._payment_intent import PaymentIntent


class EarlyFraudWarning(ListableAPIResource["EarlyFraudWarning"]):
    """
    An early fraud warning indicates that the card issuer has notified us that a
    charge may be fraudulent.

    Related guide: [Early fraud warnings](https://stripe.com/docs/disputes/measuring#early-fraud-warnings)
    """

    OBJECT_NAME: ClassVar[Literal["radar.early_fraud_warning"]] = (
        "radar.early_fraud_warning"
    )

    class ListParams(RequestOptions):
        charge: NotRequired[str]
        """
        Only return early fraud warnings for the charge specified by this charge ID.
        """
        created: NotRequired["EarlyFraudWarning.ListParamsCreated|int"]
        """
        Only return early fraud warnings that were created during the given date interval.
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
        payment_intent: NotRequired[str]
        """
        Only return early fraud warnings for charges that were created by the PaymentIntent specified by this PaymentIntent ID.
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

    class RetrieveParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    actionable: bool
    """
    An EFW is actionable if it has not received a dispute and has not been fully refunded. You may wish to proactively refund a charge that receives an EFW, in order to avoid receiving a dispute later.
    """
    charge: ExpandableField["Charge"]
    """
    ID of the charge this early fraud warning is for, optionally expanded.
    """
    created: int
    """
    Time at which the object was created. Measured in seconds since the Unix epoch.
    """
    fraud_type: str
    """
    The type of fraud labelled by the issuer. One of `card_never_received`, `fraudulent_card_application`, `made_with_counterfeit_card`, `made_with_lost_card`, `made_with_stolen_card`, `misc`, `unauthorized_use_of_card`.
    """
    id: str
    """
    Unique identifier for the object.
    """
    livemode: bool
    """
    Has the value `true` if the object exists in live mode or the value `false` if the object exists in test mode.
    """
    object: Literal["radar.early_fraud_warning"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """
    payment_intent: Optional[ExpandableField["PaymentIntent"]]
    """
    ID of the Payment Intent this early fraud warning is for, optionally expanded.
    """

    @classmethod
    def list(
        cls, **params: Unpack["EarlyFraudWarning.ListParams"]
    ) -> ListObject["EarlyFraudWarning"]:
        """
        Returns a list of early fraud warnings.
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
        cls, **params: Unpack["EarlyFraudWarning.ListParams"]
    ) -> ListObject["EarlyFraudWarning"]:
        """
        Returns a list of early fraud warnings.
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
        cls, id: str, **params: Unpack["EarlyFraudWarning.RetrieveParams"]
    ) -> "EarlyFraudWarning":
        """
        Retrieves the details of an early fraud warning that has previously been created.

        Please refer to the [early fraud warning](https://stripe.com/docs/api#early_fraud_warning_object) object reference for more details.
        """
        instance = cls(id, **params)
        instance.refresh()
        return instance

    @classmethod
    async def retrieve_async(
        cls, id: str, **params: Unpack["EarlyFraudWarning.RetrieveParams"]
    ) -> "EarlyFraudWarning":
        """
        Retrieves the details of an early fraud warning that has previously been created.

        Please refer to the [early fraud warning](https://stripe.com/docs/api#early_fraud_warning_object) object reference for more details.
        """
        instance = cls(id, **params)
        await instance.refresh_async()
        return instance

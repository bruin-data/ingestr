# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._listable_api_resource import ListableAPIResource
from stripe._request_options import RequestOptions
from stripe._stripe_object import StripeObject
from typing import ClassVar, List
from typing_extensions import Literal, NotRequired, Unpack


class PhysicalBundle(ListableAPIResource["PhysicalBundle"]):
    """
    A Physical Bundle represents the bundle of physical items - card stock, carrier letter, and envelope - that is shipped to a cardholder when you create a physical card.
    """

    OBJECT_NAME: ClassVar[Literal["issuing.physical_bundle"]] = (
        "issuing.physical_bundle"
    )

    class Features(StripeObject):
        card_logo: Literal["optional", "required", "unsupported"]
        """
        The policy for how to use card logo images in a card design with this physical bundle.
        """
        carrier_text: Literal["optional", "required", "unsupported"]
        """
        The policy for how to use carrier letter text in a card design with this physical bundle.
        """
        second_line: Literal["optional", "required", "unsupported"]
        """
        The policy for how to use a second line on a card with this physical bundle.
        """

    class ListParams(RequestOptions):
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
        status: NotRequired[Literal["active", "inactive", "review"]]
        """
        Only return physical bundles with the given status.
        """
        type: NotRequired[Literal["custom", "standard"]]
        """
        Only return physical bundles with the given type.
        """

    class RetrieveParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    features: Features
    id: str
    """
    Unique identifier for the object.
    """
    livemode: bool
    """
    Has the value `true` if the object exists in live mode or the value `false` if the object exists in test mode.
    """
    name: str
    """
    Friendly display name.
    """
    object: Literal["issuing.physical_bundle"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """
    status: Literal["active", "inactive", "review"]
    """
    Whether this physical bundle can be used to create cards.
    """
    type: Literal["custom", "standard"]
    """
    Whether this physical bundle is a standard Stripe offering or custom-made for you.
    """

    @classmethod
    def list(
        cls, **params: Unpack["PhysicalBundle.ListParams"]
    ) -> ListObject["PhysicalBundle"]:
        """
        Returns a list of physical bundle objects. The objects are sorted in descending order by creation date, with the most recently created object appearing first.
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
        cls, **params: Unpack["PhysicalBundle.ListParams"]
    ) -> ListObject["PhysicalBundle"]:
        """
        Returns a list of physical bundle objects. The objects are sorted in descending order by creation date, with the most recently created object appearing first.
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
        cls, id: str, **params: Unpack["PhysicalBundle.RetrieveParams"]
    ) -> "PhysicalBundle":
        """
        Retrieves a physical bundle object.
        """
        instance = cls(id, **params)
        instance.refresh()
        return instance

    @classmethod
    async def retrieve_async(
        cls, id: str, **params: Unpack["PhysicalBundle.RetrieveParams"]
    ) -> "PhysicalBundle":
        """
        Retrieves a physical bundle object.
        """
        instance = cls(id, **params)
        await instance.refresh_async()
        return instance

    _inner_class_types = {"features": Features}

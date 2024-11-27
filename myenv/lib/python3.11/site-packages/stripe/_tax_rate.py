# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._createable_api_resource import CreateableAPIResource
from stripe._list_object import ListObject
from stripe._listable_api_resource import ListableAPIResource
from stripe._request_options import RequestOptions
from stripe._updateable_api_resource import UpdateableAPIResource
from stripe._util import sanitize_id
from typing import ClassVar, Dict, List, Optional, cast
from typing_extensions import Literal, NotRequired, TypedDict, Unpack


class TaxRate(
    CreateableAPIResource["TaxRate"],
    ListableAPIResource["TaxRate"],
    UpdateableAPIResource["TaxRate"],
):
    """
    Tax rates can be applied to [invoices](https://stripe.com/docs/billing/invoices/tax-rates), [subscriptions](https://stripe.com/docs/billing/subscriptions/taxes) and [Checkout Sessions](https://stripe.com/docs/payments/checkout/set-up-a-subscription#tax-rates) to collect tax.

    Related guide: [Tax rates](https://stripe.com/docs/billing/taxes/tax-rates)
    """

    OBJECT_NAME: ClassVar[Literal["tax_rate"]] = "tax_rate"

    class CreateParams(RequestOptions):
        active: NotRequired[bool]
        """
        Flag determining whether the tax rate is active or inactive (archived). Inactive tax rates cannot be used with new applications or Checkout Sessions, but will still work for subscriptions and invoices that already have it set.
        """
        country: NotRequired[str]
        """
        Two-letter country code ([ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)).
        """
        description: NotRequired[str]
        """
        An arbitrary string attached to the tax rate for your internal use only. It will not be visible to your customers.
        """
        display_name: str
        """
        The display name of the tax rate, which will be shown to users.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        inclusive: bool
        """
        This specifies if the tax rate is inclusive or exclusive.
        """
        jurisdiction: NotRequired[str]
        """
        The jurisdiction for the tax rate. You can use this label field for tax reporting purposes. It also appears on your customer's invoice.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        percentage: float
        """
        This represents the tax rate percent out of 100.
        """
        state: NotRequired[str]
        """
        [ISO 3166-2 subdivision code](https://en.wikipedia.org/wiki/ISO_3166-2:US), without country prefix. For example, "NY" for New York, United States.
        """
        tax_type: NotRequired[
            Literal[
                "amusement_tax",
                "communications_tax",
                "gst",
                "hst",
                "igst",
                "jct",
                "lease_tax",
                "pst",
                "qst",
                "rst",
                "sales_tax",
                "vat",
            ]
        ]
        """
        The high-level tax type, such as `vat` or `sales_tax`.
        """

    class ListParams(RequestOptions):
        active: NotRequired[bool]
        """
        Optional flag to filter by tax rates that are either active or inactive (archived).
        """
        created: NotRequired["TaxRate.ListParamsCreated|int"]
        """
        Optional range for filtering created date.
        """
        ending_before: NotRequired[str]
        """
        A cursor for use in pagination. `ending_before` is an object ID that defines your place in the list. For instance, if you make a list request and receive 100 objects, starting with `obj_bar`, your subsequent call can include `ending_before=obj_bar` in order to fetch the previous page of the list.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        inclusive: NotRequired[bool]
        """
        Optional flag to filter by tax rates that are inclusive (or those that are not inclusive).
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

    class ModifyParams(RequestOptions):
        active: NotRequired[bool]
        """
        Flag determining whether the tax rate is active or inactive (archived). Inactive tax rates cannot be used with new applications or Checkout Sessions, but will still work for subscriptions and invoices that already have it set.
        """
        country: NotRequired[str]
        """
        Two-letter country code ([ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)).
        """
        description: NotRequired[str]
        """
        An arbitrary string attached to the tax rate for your internal use only. It will not be visible to your customers.
        """
        display_name: NotRequired[str]
        """
        The display name of the tax rate, which will be shown to users.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        jurisdiction: NotRequired[str]
        """
        The jurisdiction for the tax rate. You can use this label field for tax reporting purposes. It also appears on your customer's invoice.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        state: NotRequired[str]
        """
        [ISO 3166-2 subdivision code](https://en.wikipedia.org/wiki/ISO_3166-2:US), without country prefix. For example, "NY" for New York, United States.
        """
        tax_type: NotRequired[
            Literal[
                "amusement_tax",
                "communications_tax",
                "gst",
                "hst",
                "igst",
                "jct",
                "lease_tax",
                "pst",
                "qst",
                "rst",
                "sales_tax",
                "vat",
            ]
        ]
        """
        The high-level tax type, such as `vat` or `sales_tax`.
        """

    class RetrieveParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    active: bool
    """
    Defaults to `true`. When set to `false`, this tax rate cannot be used with new applications or Checkout Sessions, but will still work for subscriptions and invoices that already have it set.
    """
    country: Optional[str]
    """
    Two-letter country code ([ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)).
    """
    created: int
    """
    Time at which the object was created. Measured in seconds since the Unix epoch.
    """
    description: Optional[str]
    """
    An arbitrary string attached to the tax rate for your internal use only. It will not be visible to your customers.
    """
    display_name: str
    """
    The display name of the tax rates as it will appear to your customer on their receipt email, PDF, and the hosted invoice page.
    """
    effective_percentage: Optional[float]
    """
    Actual/effective tax rate percentage out of 100. For tax calculations with automatic_tax[enabled]=true,
    this percentage reflects the rate actually used to calculate tax based on the product's taxability
    and whether the user is registered to collect taxes in the corresponding jurisdiction.
    """
    id: str
    """
    Unique identifier for the object.
    """
    inclusive: bool
    """
    This specifies if the tax rate is inclusive or exclusive.
    """
    jurisdiction: Optional[str]
    """
    The jurisdiction for the tax rate. You can use this label field for tax reporting purposes. It also appears on your customer's invoice.
    """
    jurisdiction_level: Optional[
        Literal["city", "country", "county", "district", "multiple", "state"]
    ]
    """
    The level of the jurisdiction that imposes this tax rate. Will be `null` for manually defined tax rates.
    """
    livemode: bool
    """
    Has the value `true` if the object exists in live mode or the value `false` if the object exists in test mode.
    """
    metadata: Optional[Dict[str, str]]
    """
    Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format.
    """
    object: Literal["tax_rate"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """
    percentage: float
    """
    Tax rate percentage out of 100. For tax calculations with automatic_tax[enabled]=true, this percentage includes the statutory tax rate of non-taxable jurisdictions.
    """
    state: Optional[str]
    """
    [ISO 3166-2 subdivision code](https://en.wikipedia.org/wiki/ISO_3166-2:US), without country prefix. For example, "NY" for New York, United States.
    """
    tax_type: Optional[
        Literal[
            "amusement_tax",
            "communications_tax",
            "gst",
            "hst",
            "igst",
            "jct",
            "lease_tax",
            "pst",
            "qst",
            "rst",
            "sales_tax",
            "vat",
        ]
    ]
    """
    The high-level tax type, such as `vat` or `sales_tax`.
    """

    @classmethod
    def create(cls, **params: Unpack["TaxRate.CreateParams"]) -> "TaxRate":
        """
        Creates a new tax rate.
        """
        return cast(
            "TaxRate",
            cls._static_request(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    async def create_async(
        cls, **params: Unpack["TaxRate.CreateParams"]
    ) -> "TaxRate":
        """
        Creates a new tax rate.
        """
        return cast(
            "TaxRate",
            await cls._static_request_async(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    def list(
        cls, **params: Unpack["TaxRate.ListParams"]
    ) -> ListObject["TaxRate"]:
        """
        Returns a list of your tax rates. Tax rates are returned sorted by creation date, with the most recently created tax rates appearing first.
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
        cls, **params: Unpack["TaxRate.ListParams"]
    ) -> ListObject["TaxRate"]:
        """
        Returns a list of your tax rates. Tax rates are returned sorted by creation date, with the most recently created tax rates appearing first.
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
        cls, id: str, **params: Unpack["TaxRate.ModifyParams"]
    ) -> "TaxRate":
        """
        Updates an existing tax rate.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(id))
        return cast(
            "TaxRate",
            cls._static_request(
                "post",
                url,
                params=params,
            ),
        )

    @classmethod
    async def modify_async(
        cls, id: str, **params: Unpack["TaxRate.ModifyParams"]
    ) -> "TaxRate":
        """
        Updates an existing tax rate.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(id))
        return cast(
            "TaxRate",
            await cls._static_request_async(
                "post",
                url,
                params=params,
            ),
        )

    @classmethod
    def retrieve(
        cls, id: str, **params: Unpack["TaxRate.RetrieveParams"]
    ) -> "TaxRate":
        """
        Retrieves a tax rate with the given ID
        """
        instance = cls(id, **params)
        instance.refresh()
        return instance

    @classmethod
    async def retrieve_async(
        cls, id: str, **params: Unpack["TaxRate.RetrieveParams"]
    ) -> "TaxRate":
        """
        Retrieves a tax rate with the given ID
        """
        instance = cls(id, **params)
        await instance.refresh_async()
        return instance

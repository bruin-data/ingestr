# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._createable_api_resource import CreateableAPIResource
from stripe._expandable_field import ExpandableField
from stripe._list_object import ListObject
from stripe._listable_api_resource import ListableAPIResource
from stripe._request_options import RequestOptions
from stripe._stripe_object import StripeObject
from stripe._util import class_method_variant, sanitize_id
from typing import ClassVar, List, Optional, cast, overload
from typing_extensions import (
    Literal,
    NotRequired,
    TypedDict,
    Unpack,
    TYPE_CHECKING,
)

if TYPE_CHECKING:
    from stripe._customer import Customer
    from stripe.billing._meter import Meter


class Alert(CreateableAPIResource["Alert"], ListableAPIResource["Alert"]):
    """
    A billing alert is a resource that notifies you when a certain usage threshold on a meter is crossed. For example, you might create a billing alert to notify you when a certain user made 100 API requests.
    """

    OBJECT_NAME: ClassVar[Literal["billing.alert"]] = "billing.alert"

    class Filter(StripeObject):
        customer: Optional[ExpandableField["Customer"]]
        """
        Limit the scope of the alert to this customer ID
        """

    class UsageThresholdConfig(StripeObject):
        gte: int
        """
        The value at which this alert will trigger.
        """
        meter: ExpandableField["Meter"]
        """
        The [Billing Meter](https://stripe.com/api/billing/meter) ID whose usage is monitored.
        """
        recurrence: Literal["one_time"]
        """
        Defines how the alert will behave.
        """

    class ActivateParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class ArchiveParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class CreateParams(RequestOptions):
        alert_type: Literal["usage_threshold"]
        """
        The type of alert to create.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        filter: NotRequired["Alert.CreateParamsFilter"]
        """
        Filters to limit the scope of an alert.
        """
        title: str
        """
        The title of the alert.
        """
        usage_threshold_config: NotRequired[
            "Alert.CreateParamsUsageThresholdConfig"
        ]
        """
        The configuration of the usage threshold.
        """

    class CreateParamsFilter(TypedDict):
        customer: NotRequired[str]
        """
        Limit the scope to this alert only to this customer.
        """

    class CreateParamsUsageThresholdConfig(TypedDict):
        gte: int
        """
        Defines at which value the alert will fire.
        """
        meter: NotRequired[str]
        """
        The [Billing Meter](https://stripe.com/api/billing/meter) ID whose usage is monitored.
        """
        recurrence: Literal["one_time"]
        """
        Whether the alert should only fire only once, or once per billing cycle.
        """

    class DeactivateParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class ListParams(RequestOptions):
        alert_type: NotRequired[Literal["usage_threshold"]]
        """
        Filter results to only include this type of alert.
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
        meter: NotRequired[str]
        """
        Filter results to only include alerts with the given meter.
        """
        starting_after: NotRequired[str]
        """
        A cursor for use in pagination. `starting_after` is an object ID that defines your place in the list. For instance, if you make a list request and receive 100 objects, ending with `obj_foo`, your subsequent call can include `starting_after=obj_foo` in order to fetch the next page of the list.
        """

    class RetrieveParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    alert_type: Literal["usage_threshold"]
    """
    Defines the type of the alert.
    """
    filter: Optional[Filter]
    """
    Limits the scope of the alert to a specific [customer](https://stripe.com/docs/api/customers).
    """
    id: str
    """
    Unique identifier for the object.
    """
    livemode: bool
    """
    Has the value `true` if the object exists in live mode or the value `false` if the object exists in test mode.
    """
    object: Literal["billing.alert"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """
    status: Optional[Literal["active", "archived", "inactive"]]
    """
    Status of the alert. This can be active, inactive or archived.
    """
    title: str
    """
    Title of the alert.
    """
    usage_threshold_config: Optional[UsageThresholdConfig]
    """
    Encapsulates configuration of the alert to monitor usage on a specific [Billing Meter](https://stripe.com/docs/api/billing/meter).
    """

    @classmethod
    def _cls_activate(
        cls, id: str, **params: Unpack["Alert.ActivateParams"]
    ) -> "Alert":
        """
        Reactivates this alert, allowing it to trigger again.
        """
        return cast(
            "Alert",
            cls._static_request(
                "post",
                "/v1/billing/alerts/{id}/activate".format(id=sanitize_id(id)),
                params=params,
            ),
        )

    @overload
    @staticmethod
    def activate(id: str, **params: Unpack["Alert.ActivateParams"]) -> "Alert":
        """
        Reactivates this alert, allowing it to trigger again.
        """
        ...

    @overload
    def activate(self, **params: Unpack["Alert.ActivateParams"]) -> "Alert":
        """
        Reactivates this alert, allowing it to trigger again.
        """
        ...

    @class_method_variant("_cls_activate")
    def activate(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Alert.ActivateParams"]
    ) -> "Alert":
        """
        Reactivates this alert, allowing it to trigger again.
        """
        return cast(
            "Alert",
            self._request(
                "post",
                "/v1/billing/alerts/{id}/activate".format(
                    id=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    async def _cls_activate_async(
        cls, id: str, **params: Unpack["Alert.ActivateParams"]
    ) -> "Alert":
        """
        Reactivates this alert, allowing it to trigger again.
        """
        return cast(
            "Alert",
            await cls._static_request_async(
                "post",
                "/v1/billing/alerts/{id}/activate".format(id=sanitize_id(id)),
                params=params,
            ),
        )

    @overload
    @staticmethod
    async def activate_async(
        id: str, **params: Unpack["Alert.ActivateParams"]
    ) -> "Alert":
        """
        Reactivates this alert, allowing it to trigger again.
        """
        ...

    @overload
    async def activate_async(
        self, **params: Unpack["Alert.ActivateParams"]
    ) -> "Alert":
        """
        Reactivates this alert, allowing it to trigger again.
        """
        ...

    @class_method_variant("_cls_activate_async")
    async def activate_async(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Alert.ActivateParams"]
    ) -> "Alert":
        """
        Reactivates this alert, allowing it to trigger again.
        """
        return cast(
            "Alert",
            await self._request_async(
                "post",
                "/v1/billing/alerts/{id}/activate".format(
                    id=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    def _cls_archive(
        cls, id: str, **params: Unpack["Alert.ArchiveParams"]
    ) -> "Alert":
        """
        Archives this alert, removing it from the list view and APIs. This is non-reversible.
        """
        return cast(
            "Alert",
            cls._static_request(
                "post",
                "/v1/billing/alerts/{id}/archive".format(id=sanitize_id(id)),
                params=params,
            ),
        )

    @overload
    @staticmethod
    def archive(id: str, **params: Unpack["Alert.ArchiveParams"]) -> "Alert":
        """
        Archives this alert, removing it from the list view and APIs. This is non-reversible.
        """
        ...

    @overload
    def archive(self, **params: Unpack["Alert.ArchiveParams"]) -> "Alert":
        """
        Archives this alert, removing it from the list view and APIs. This is non-reversible.
        """
        ...

    @class_method_variant("_cls_archive")
    def archive(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Alert.ArchiveParams"]
    ) -> "Alert":
        """
        Archives this alert, removing it from the list view and APIs. This is non-reversible.
        """
        return cast(
            "Alert",
            self._request(
                "post",
                "/v1/billing/alerts/{id}/archive".format(
                    id=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    async def _cls_archive_async(
        cls, id: str, **params: Unpack["Alert.ArchiveParams"]
    ) -> "Alert":
        """
        Archives this alert, removing it from the list view and APIs. This is non-reversible.
        """
        return cast(
            "Alert",
            await cls._static_request_async(
                "post",
                "/v1/billing/alerts/{id}/archive".format(id=sanitize_id(id)),
                params=params,
            ),
        )

    @overload
    @staticmethod
    async def archive_async(
        id: str, **params: Unpack["Alert.ArchiveParams"]
    ) -> "Alert":
        """
        Archives this alert, removing it from the list view and APIs. This is non-reversible.
        """
        ...

    @overload
    async def archive_async(
        self, **params: Unpack["Alert.ArchiveParams"]
    ) -> "Alert":
        """
        Archives this alert, removing it from the list view and APIs. This is non-reversible.
        """
        ...

    @class_method_variant("_cls_archive_async")
    async def archive_async(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Alert.ArchiveParams"]
    ) -> "Alert":
        """
        Archives this alert, removing it from the list view and APIs. This is non-reversible.
        """
        return cast(
            "Alert",
            await self._request_async(
                "post",
                "/v1/billing/alerts/{id}/archive".format(
                    id=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    def create(cls, **params: Unpack["Alert.CreateParams"]) -> "Alert":
        """
        Creates a billing alert
        """
        return cast(
            "Alert",
            cls._static_request(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    async def create_async(
        cls, **params: Unpack["Alert.CreateParams"]
    ) -> "Alert":
        """
        Creates a billing alert
        """
        return cast(
            "Alert",
            await cls._static_request_async(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    def _cls_deactivate(
        cls, id: str, **params: Unpack["Alert.DeactivateParams"]
    ) -> "Alert":
        """
        Deactivates this alert, preventing it from triggering.
        """
        return cast(
            "Alert",
            cls._static_request(
                "post",
                "/v1/billing/alerts/{id}/deactivate".format(
                    id=sanitize_id(id)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    def deactivate(
        id: str, **params: Unpack["Alert.DeactivateParams"]
    ) -> "Alert":
        """
        Deactivates this alert, preventing it from triggering.
        """
        ...

    @overload
    def deactivate(
        self, **params: Unpack["Alert.DeactivateParams"]
    ) -> "Alert":
        """
        Deactivates this alert, preventing it from triggering.
        """
        ...

    @class_method_variant("_cls_deactivate")
    def deactivate(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Alert.DeactivateParams"]
    ) -> "Alert":
        """
        Deactivates this alert, preventing it from triggering.
        """
        return cast(
            "Alert",
            self._request(
                "post",
                "/v1/billing/alerts/{id}/deactivate".format(
                    id=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    async def _cls_deactivate_async(
        cls, id: str, **params: Unpack["Alert.DeactivateParams"]
    ) -> "Alert":
        """
        Deactivates this alert, preventing it from triggering.
        """
        return cast(
            "Alert",
            await cls._static_request_async(
                "post",
                "/v1/billing/alerts/{id}/deactivate".format(
                    id=sanitize_id(id)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    async def deactivate_async(
        id: str, **params: Unpack["Alert.DeactivateParams"]
    ) -> "Alert":
        """
        Deactivates this alert, preventing it from triggering.
        """
        ...

    @overload
    async def deactivate_async(
        self, **params: Unpack["Alert.DeactivateParams"]
    ) -> "Alert":
        """
        Deactivates this alert, preventing it from triggering.
        """
        ...

    @class_method_variant("_cls_deactivate_async")
    async def deactivate_async(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Alert.DeactivateParams"]
    ) -> "Alert":
        """
        Deactivates this alert, preventing it from triggering.
        """
        return cast(
            "Alert",
            await self._request_async(
                "post",
                "/v1/billing/alerts/{id}/deactivate".format(
                    id=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    def list(cls, **params: Unpack["Alert.ListParams"]) -> ListObject["Alert"]:
        """
        Lists billing active and inactive alerts
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
        cls, **params: Unpack["Alert.ListParams"]
    ) -> ListObject["Alert"]:
        """
        Lists billing active and inactive alerts
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
        cls, id: str, **params: Unpack["Alert.RetrieveParams"]
    ) -> "Alert":
        """
        Retrieves a billing alert given an ID
        """
        instance = cls(id, **params)
        instance.refresh()
        return instance

    @classmethod
    async def retrieve_async(
        cls, id: str, **params: Unpack["Alert.RetrieveParams"]
    ) -> "Alert":
        """
        Retrieves a billing alert given an ID
        """
        instance = cls(id, **params)
        await instance.refresh_async()
        return instance

    _inner_class_types = {
        "filter": Filter,
        "usage_threshold_config": UsageThresholdConfig,
    }

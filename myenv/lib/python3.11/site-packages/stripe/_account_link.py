# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._createable_api_resource import CreateableAPIResource
from stripe._request_options import RequestOptions
from typing import ClassVar, List, cast
from typing_extensions import Literal, NotRequired, TypedDict, Unpack


class AccountLink(CreateableAPIResource["AccountLink"]):
    """
    Account Links are the means by which a Connect platform grants a connected account permission to access
    Stripe-hosted applications, such as Connect Onboarding.

    Related guide: [Connect Onboarding](https://stripe.com/docs/connect/custom/hosted-onboarding)
    """

    OBJECT_NAME: ClassVar[Literal["account_link"]] = "account_link"

    class CreateParams(RequestOptions):
        account: str
        """
        The identifier of the account to create an account link for.
        """
        collect: NotRequired[Literal["currently_due", "eventually_due"]]
        """
        The collect parameter is deprecated. Use `collection_options` instead.
        """
        collection_options: NotRequired[
            "AccountLink.CreateParamsCollectionOptions"
        ]
        """
        Specifies the requirements that Stripe collects from connected accounts in the Connect Onboarding flow.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        refresh_url: NotRequired[str]
        """
        The URL the user will be redirected to if the account link is expired, has been previously-visited, or is otherwise invalid. The URL you specify should attempt to generate a new account link with the same parameters used to create the original account link, then redirect the user to the new account link's URL so they can continue with Connect Onboarding. If a new account link cannot be generated or the redirect fails you should display a useful error to the user.
        """
        return_url: NotRequired[str]
        """
        The URL that the user will be redirected to upon leaving or completing the linked flow.
        """
        type: Literal["account_onboarding", "account_update"]
        """
        The type of account link the user is requesting. Possible values are `account_onboarding` or `account_update`.
        """

    class CreateParamsCollectionOptions(TypedDict):
        fields: Literal["currently_due", "eventually_due"]
        """
        Specifies whether the platform collects only currently_due requirements (`currently_due`) or both currently_due and eventually_due requirements (`eventually_due`). If you don't specify `collection_options`, the default value is `currently_due`.
        """
        future_requirements: NotRequired[Literal["include", "omit"]]
        """
        Specifies whether the platform collects future_requirements in addition to requirements in Connect Onboarding. The default value is `omit`.
        """

    created: int
    """
    Time at which the object was created. Measured in seconds since the Unix epoch.
    """
    expires_at: int
    """
    The timestamp at which this account link will expire.
    """
    object: Literal["account_link"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """
    url: str
    """
    The URL for the account link.
    """

    @classmethod
    def create(
        cls, **params: Unpack["AccountLink.CreateParams"]
    ) -> "AccountLink":
        """
        Creates an AccountLink object that includes a single-use Stripe URL that the platform can redirect their user to in order to take them through the Connect Onboarding flow.
        """
        return cast(
            "AccountLink",
            cls._static_request(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    async def create_async(
        cls, **params: Unpack["AccountLink.CreateParams"]
    ) -> "AccountLink":
        """
        Creates an AccountLink object that includes a single-use Stripe URL that the platform can redirect their user to in order to take them through the Connect Onboarding flow.
        """
        return cast(
            "AccountLink",
            await cls._static_request_async(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

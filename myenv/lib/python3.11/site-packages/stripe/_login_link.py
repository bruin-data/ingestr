# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._stripe_object import StripeObject
from typing import ClassVar
from typing_extensions import Literal


class LoginLink(StripeObject):
    """
    Login Links are single-use URLs for a connected account to access the Express Dashboard. The connected account's [account.controller.stripe_dashboard.type](https://stripe.com/api/accounts/object#account_object-controller-stripe_dashboard-type) must be `express` to have access to the Express Dashboard.
    """

    OBJECT_NAME: ClassVar[Literal["login_link"]] = "login_link"
    created: int
    """
    Time at which the object was created. Measured in seconds since the Unix epoch.
    """
    object: Literal["login_link"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """
    url: str
    """
    The URL for the login link.
    """

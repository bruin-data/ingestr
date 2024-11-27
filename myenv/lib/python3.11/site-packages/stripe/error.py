# -*- coding: utf-8 -*-
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.error_object package is deprecated, please change your
    imports to import from stripe directly.
    From:
      from stripe.api_resources.error_object import ErrorObject
    To:
      from stripe import ErrorObject
    """,
    DeprecationWarning,
)

if not TYPE_CHECKING:
    from stripe._error import StripeError  # noqa
    from stripe._error import APIError  # noqa
    from stripe._error import APIConnectionError  # noqa
    from stripe._error import StripeErrorWithParamCode  # noqa
    from stripe._error import CardError  # noqa
    from stripe._error import IdempotencyError  # noqa
    from stripe._error import InvalidRequestError  # noqa
    from stripe._error import AuthenticationError  # noqa
    from stripe._error import PermissionError  # noqa
    from stripe._error import RateLimitError  # noqa
    from stripe._error import SignatureVerificationError  # noqa

# -*- coding: utf-8 -*-
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.util package is deprecated, please change your
    imports to import from stripe directly.
    From:
      from stripe.util import convert_to_stripe_object
    To:
      from stripe import convert_to_stripe_object
    """,
    DeprecationWarning,
    stacklevel=2,
)

if not TYPE_CHECKING:
    from stripe._util import *  # noqa

# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.ephemeral_key package is deprecated, please change your
    imports to import from stripe directly.
    From:
      from stripe.api_resources.ephemeral_key import EphemeralKey
    To:
      from stripe import EphemeralKey
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe._ephemeral_key import (  # noqa
        EphemeralKey,
    )

# -*- coding: utf-8 -*-
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.oauth package is deprecated, please change your
    imports to import from stripe directly.
    From:
      from stripe.oauth import OAuth
    To:
      from stripe import OAuth
    """,
    DeprecationWarning,
    stacklevel=2,
)

if not TYPE_CHECKING:
    from stripe._oauth import (  # noqa
        OAuth,
    )

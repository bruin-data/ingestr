# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.file_link package is deprecated, please change your
    imports to import from stripe directly.
    From:
      from stripe.api_resources.file_link import FileLink
    To:
      from stripe import FileLink
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe._file_link import (  # noqa
        FileLink,
    )

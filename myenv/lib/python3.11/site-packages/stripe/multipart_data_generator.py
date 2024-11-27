# -*- coding: utf-8 -*-
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.multipart_data_generator package is deprecated and will become internal in the future.
    """,
    DeprecationWarning,
)

if not TYPE_CHECKING:
    from stripe._multipart_data_generator import (  # noqa
        MultipartDataGenerator,
    )

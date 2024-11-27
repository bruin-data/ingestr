# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.country_spec package is deprecated, please change your
    imports to import from stripe directly.
    From:
      from stripe.api_resources.country_spec import CountrySpec
    To:
      from stripe import CountrySpec
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe._country_spec import (  # noqa
        CountrySpec,
    )

# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.sigma package is deprecated, please change your
    imports to import from stripe.sigma directly.
    From:
      from stripe.api_resources.sigma import ...
    To:
      from stripe.sigma import ...
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe.api_resources.sigma.scheduled_query_run import (
        ScheduledQueryRun,
    )

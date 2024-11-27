# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.reporting.report_run package is deprecated, please change your
    imports to import from stripe.reporting directly.
    From:
      from stripe.api_resources.reporting.report_run import ReportRun
    To:
      from stripe.reporting import ReportRun
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe.reporting._report_run import (  # noqa
        ReportRun,
    )

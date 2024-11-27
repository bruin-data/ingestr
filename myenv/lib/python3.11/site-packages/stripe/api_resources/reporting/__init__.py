# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.reporting package is deprecated, please change your
    imports to import from stripe.reporting directly.
    From:
      from stripe.api_resources.reporting import ...
    To:
      from stripe.reporting import ...
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe.api_resources.reporting.report_run import ReportRun
    from stripe.api_resources.reporting.report_type import ReportType

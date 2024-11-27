# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.issuing package is deprecated, please change your
    imports to import from stripe.issuing directly.
    From:
      from stripe.api_resources.issuing import ...
    To:
      from stripe.issuing import ...
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe.api_resources.issuing.authorization import Authorization
    from stripe.api_resources.issuing.card import Card
    from stripe.api_resources.issuing.cardholder import Cardholder
    from stripe.api_resources.issuing.dispute import Dispute
    from stripe.api_resources.issuing.personalization_design import (
        PersonalizationDesign,
    )
    from stripe.api_resources.issuing.physical_bundle import PhysicalBundle
    from stripe.api_resources.issuing.token import Token
    from stripe.api_resources.issuing.transaction import Transaction

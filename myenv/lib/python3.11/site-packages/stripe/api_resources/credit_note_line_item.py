# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.credit_note_line_item package is deprecated, please change your
    imports to import from stripe directly.
    From:
      from stripe.api_resources.credit_note_line_item import CreditNoteLineItem
    To:
      from stripe import CreditNoteLineItem
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe._credit_note_line_item import (  # noqa
        CreditNoteLineItem,
    )

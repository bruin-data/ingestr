# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.treasury package is deprecated, please change your
    imports to import from stripe.treasury directly.
    From:
      from stripe.api_resources.treasury import ...
    To:
      from stripe.treasury import ...
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe.api_resources.treasury.credit_reversal import CreditReversal
    from stripe.api_resources.treasury.debit_reversal import DebitReversal
    from stripe.api_resources.treasury.financial_account import (
        FinancialAccount,
    )
    from stripe.api_resources.treasury.financial_account_features import (
        FinancialAccountFeatures,
    )
    from stripe.api_resources.treasury.inbound_transfer import InboundTransfer
    from stripe.api_resources.treasury.outbound_payment import OutboundPayment
    from stripe.api_resources.treasury.outbound_transfer import (
        OutboundTransfer,
    )
    from stripe.api_resources.treasury.received_credit import ReceivedCredit
    from stripe.api_resources.treasury.received_debit import ReceivedDebit
    from stripe.api_resources.treasury.transaction import Transaction
    from stripe.api_resources.treasury.transaction_entry import (
        TransactionEntry,
    )

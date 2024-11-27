from stripe._stripe_object import StripeObject

from warnings import warn

warn(
    """
    The RecipientTransfer class is deprecated and will be removed in a future
    """,
    DeprecationWarning,
    stacklevel=2,
)


class RecipientTransfer(StripeObject):
    OBJECT_NAME = "recipient_transfer"

# Putting a selection of model classes into pyairtable.models
# is how we indicate the "public API" vs. implementation details.
# If it's not in here, we don't expect implementers to call it directly.

"""
pyAirtable will wrap certain API responses in type-annotated models,
some of which will be deeply nested within each other. Models which
implementers can interact with directly are documented below.
"""

from .collaborator import Collaborator
from .comment import Comment
from .webhook import Webhook, WebhookNotification, WebhookPayload

__all__ = [
    "Collaborator",
    "Comment",
    "Webhook",
    "WebhookNotification",
    "WebhookPayload",
]

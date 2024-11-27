from typing import Optional

from typing_extensions import TypeAlias

from ._base import AirtableModel

UserId: TypeAlias = str


class Collaborator(AirtableModel):
    """
    Represents an Airtable user being passed from the API.

    This is only used in contexts involving other models (e.g. :class:`~pyairtable.models.Comment`).
    In contexts where API values are returned as ``dict``, we will return
    collaborator information as a ``dict`` as well.
    """

    #: Airtable's unique ID for the user, in the format ``usrXXXXXXXXXXXXXX``.
    id: UserId

    #: The email address of the user.
    email: Optional[str]

    #: The display name of the user.
    name: Optional[str]

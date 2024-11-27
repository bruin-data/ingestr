import inspect
import re
import textwrap
from datetime import date, datetime
from functools import partial, wraps
from typing import (
    Any,
    Callable,
    Generic,
    Iterable,
    Iterator,
    List,
    Optional,
    Sequence,
    TypeVar,
    Union,
    cast,
)

import requests
from typing_extensions import ParamSpec, Protocol

from pyairtable.api.types import CreateAttachmentDict

P = ParamSpec("P")
R = TypeVar("R", covariant=True)
T = TypeVar("T")


def datetime_to_iso_str(value: datetime) -> str:
    """
    Convert ``datetime`` object into Airtable compatible ISO 8601 string
    e.g. "2014-09-05T12:34:56.000Z"

    Args:
        value: datetime object
    """
    return value.isoformat(timespec="milliseconds").replace("+00:00", "Z")


def datetime_from_iso_str(value: str) -> datetime:
    """
    Convert an ISO 8601 datetime string into a ``datetime`` object.

    Args:
        value: datetime string, e.g. "2014-09-05T07:00:00.000Z"
    """
    if value.endswith("Z"):
        value = value[:-1] + "+00:00"
    return datetime.fromisoformat(value)


def date_to_iso_str(value: Union[date, datetime]) -> str:
    """
    Convert a ``date`` or ``datetime`` into an Airtable-compatible ISO 8601 string

    Args:
        value: date or datetime object, e.g. "2014-09-05"
    """
    return value.strftime("%Y-%m-%d")


def date_from_iso_str(value: str) -> date:
    """
    Convert ISO 8601 date string into a ``date`` object.

    Args:
        value: date string, e.g. "2014-09-05"
    """
    return datetime.strptime(value, "%Y-%m-%d").date()


def attachment(url: str, filename: str = "") -> CreateAttachmentDict:
    """
    Build a ``dict`` in the expected format for creating attachments.

    When creating an attachment, ``url`` is required, and ``filename`` is optional.
    Airtable will download the file at the given url and keep its own copy of it.
    All other attachment object properties will be generated server-side soon afterward.

    Note:
        Attachment field values **must** be an array of
        :class:`~pyairtable.api.types.AttachmentDict` or
        :class:`~pyairtable.api.types.CreateAttachmentDict`;
        it is not valid to pass a single item to the API.

    Usage:
        >>> table = Table(...)
        >>> profile_url = "https://myprofile.com/id/profile.jpg
        >>> rec = table.create({"Profile Photo": [attachment(profile_url)]})
        {
            'id': 'recZXOZ5gT9vVGHfL',
            'fields': {
                'attachment': [
                    {
                        'id': 'attu6kbaST3wUuNTA',
                        'url': 'https://aws1.discourse-cdn.com/airtable/original/2X/4/411e4fac00df06a5e316a0585a831549e11d0705.png',
                        'filename': '411e4fac00df06a5e316a0585a831549e11d0705.png'
                    }
                ]
            },
            'createdTime': '2021-08-21T22:28:36.000Z'
        }


    """
    return {"url": url} if not filename else {"url": url, "filename": filename}


def chunked(iterable: Sequence[T], chunk_size: int) -> Iterator[Sequence[T]]:
    """
    Break a sequence into chunks.

    Args:
        iterable: Any sequence.
        chunk_size: Maximum items to yield per chunk.
    """
    for i in range(0, len(iterable), chunk_size):
        yield iterable[i : i + chunk_size]


def is_airtable_id(value: Any, prefix: str = "") -> bool:
    """
    Check whether the given value is an Airtable ID.

    Args:
        value: The value to check.
        prefix: If provided, the ID must have the given prefix.
    """
    if not isinstance(value, str):
        return False
    if prefix and not value.startswith(prefix):
        return False
    return len(value) == 17


is_record_id = partial(is_airtable_id, prefix="rec")
is_base_id = partial(is_airtable_id, prefix="app")
is_table_id = partial(is_airtable_id, prefix="tbl")
is_field_id = partial(is_airtable_id, prefix="fld")
is_user_id = partial(is_airtable_id, prefix="usr")


F = TypeVar("F", bound=Callable[..., Any])


def enterprise_only(wrapped: F, /, modify_docstring: bool = True) -> F:
    """
    Wrap a function or method so that if Airtable returns a 404,
    we will annotate the error with a helpful note to the user.
    """

    if modify_docstring:
        _prepend_docstring_text(wrapped, "|enterprise_only|")

    # Allow putting the decorator on a class
    if inspect.isclass(wrapped):
        for name, obj in vars(wrapped).items():
            if inspect.isfunction(obj):
                setattr(wrapped, name, enterprise_only(obj))
        return cast(F, wrapped)

    @wraps(wrapped)
    def _decorated(*args: Any, **kwargs: Any) -> Any:
        try:
            return wrapped(*args, **kwargs)
        except requests.exceptions.HTTPError as exc:
            if exc.response is not None and exc.response.status_code == 404:
                exc.args = (
                    *exc.args,
                    f"NOTE: {wrapped.__qualname__}() requires an enterprise billing plan.",
                )
            raise exc

    return _decorated  # type: ignore[return-value]


def _prepend_docstring_text(obj: Any, text: str) -> None:
    if not (doc := obj.__doc__):
        return
    doc = doc.lstrip("\n")
    if has_leading_spaces := re.match(r"^\s+", doc):
        text = textwrap.indent(text, has_leading_spaces[0])
    obj.__doc__ = f"{text}\n\n{doc}"


def _append_docstring_text(obj: Any, text: str) -> None:
    if not (doc := obj.__doc__):
        return
    doc = doc.rstrip("\n")
    if has_leading_spaces := re.match(r"^\s+", doc):
        text = textwrap.indent(text, has_leading_spaces[0])
    obj.__doc__ = f"{doc}\n\n{text}"


class FetchMethod(Protocol, Generic[R]):
    def __get__(self, instance: Any, owner: Any) -> Callable[..., R]: ...

    def __call__(self_, self: Any, *, force: bool = False) -> R: ...


def cache_unless_forced(func: Callable[P, R]) -> FetchMethod[R]:
    """
    Wrap a method (e.g. ``Base.shares()``) in a decorator that will save
    a memoized version of the return value for future reuse, but will also
    allow callers to pass ``force=True`` to recompute the memoized version.
    """

    attr = f"_{func.__name__}"
    if attr.startswith("__"):
        attr = "_cached_" + attr.lstrip("_")

    @wraps(func)
    def _inner(self: Any, *, force: bool = False) -> R:
        if force or getattr(self, attr, None) is None:
            setattr(self, attr, func(self))
        return cast(R, getattr(self, attr))

    _inner.__annotations__["force"] = bool
    _append_docstring_text(_inner, "Args:\n\tforce: |kwarg_force_metadata|")

    return _inner


def coerce_iso_str(value: Any) -> Optional[str]:
    """
    Given an input that might be a date or datetime, or an ISO 8601 formatted str,
    convert the value into an ISO 8601 formatted str.
    """
    if value is None:
        return value
    if isinstance(value, str):
        datetime.fromisoformat(value)  # validates type, nothing more
        return value
    if isinstance(value, (date, datetime)):
        return value.isoformat()
    raise TypeError(f"cannot coerce {type(value)} into ISO 8601 str")


def coerce_list_str(value: Optional[Union[str, Iterable[str]]]) -> List[str]:
    """
    Given an input that is either a str or an iterable of str, return a list.
    """
    if value is None:
        return []
    if isinstance(value, str):
        return [value]
    return list(value)

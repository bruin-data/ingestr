from typing import Any, Collection, Optional, Tuple, Union

from requests import Session
from requests.adapters import HTTPAdapter
from urllib3.util.retry import Retry

DEFAULT_RETRIABLE_STATUS_CODES = (429,)
DEFAULT_BACKOFF_FACTOR = 0.1  # retry after 0.1, 0.2, 0.4, 0.8, 1.6 seconds
DEFAULT_MAX_RETRIES = 5


def retry_strategy(
    *,
    status_forcelist: Tuple[int, ...] = DEFAULT_RETRIABLE_STATUS_CODES,
    backoff_factor: Union[int, float] = DEFAULT_BACKOFF_FACTOR,
    total: int = DEFAULT_MAX_RETRIES,
    allowed_methods: Optional[Collection[str]] = None,
    **kwargs: Any,
) -> Retry:
    """
    Create a `Retry <https://urllib3.readthedocs.io/en/stable/reference/urllib3.util.html#urllib3.util.Retry>`_
    instance with adjustable default values. :class:`~pyairtable.Api` accepts this via the
    ``retry_strategy=`` parameter.

    For example, to increase the total number of retries:

        >>> from pyairtable import Api, retry_strategy
        >>> api = Api('auth_token', retry_strategy=retry_strategy(total=10))

    Or to retry certain types of server errors in addition to rate limiting errors:

        >>> from pyairtable import Api, retry_strategy
        >>> retry = retry_strategy(status_forcelist=(429, 500, 502, 503, 504))
        >>> api = Api('auth_token', retry_strategy=retry)

    You can also disable retries entirely:

        >>> from pyairtable import Api
        >>> api = Api('auth_token', retry_strategy=None)

    .. versionadded:: 1.4.0

    Args:
        status_forcelist: Status codes which should be retried.
        allowed_methods: HTTP methods which can be retried.
            If ``None``, then all HTTP methods will be retried.
        backoff_factor:
            A backoff factor to apply between attempts after the second try.
            Sleep time between each request will be calculated as
            ``backoff_factor * (2 ** (retry_count - 1))``
        total:
            Maximum number of retries. Note that ``0`` means no retries,
            whereas ``1`` will execute a total of two requests (original + 1 retry).
        **kwargs: Accepts any valid parameter to `Retry`_.
    """
    return Retry(
        total=total,
        backoff_factor=backoff_factor,
        status_forcelist=status_forcelist,
        allowed_methods=allowed_methods,
        **kwargs,
    )


class _RetryingSession(Session):
    def __init__(self, retry_strategy: Retry):
        super().__init__()

        adapter = HTTPAdapter(max_retries=retry_strategy)

        self.mount("https://", adapter)
        self.mount("http://", adapter)


__all__ = [
    "Retry",
    "_RetryingSession",
]

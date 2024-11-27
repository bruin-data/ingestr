import logging
import time
import typing
from enum import Enum
from typing import List, Optional, Tuple, Union

# We only use this import for type hinting
try:
    # If urllib3~=2.0 is installed
    from urllib3 import BaseHTTPResponse  # type: ignore
except ImportError:
    # If urllib3~=1.0 is installed
    from urllib3 import HTTPResponse as BaseHTTPResponse
from urllib3 import Retry
from urllib3.util.retry import RequestHistory

from databricks.sql.exc import (
    CursorAlreadyClosedError,
    MaxRetryDurationError,
    NonRecoverableNetworkError,
    OperationalError,
    SessionAlreadyClosedError,
    UnsafeToRetryError,
)

logger = logging.getLogger(__name__)


class CommandType(Enum):
    EXECUTE_STATEMENT = "ExecuteStatement"
    CLOSE_SESSION = "CloseSession"
    CLOSE_OPERATION = "CloseOperation"
    OTHER = "Other"

    @classmethod
    def get(cls, value: str):
        value_name_map = {i.value: i.name for i in cls}
        valid_command = value_name_map.get(value, False)
        if valid_command:
            return getattr(cls, str(valid_command))
        else:
            return cls.OTHER


class DatabricksRetryPolicy(Retry):
    """
    Implements our v3 retry policy by extending urllib3's robust default retry behaviour.

    Retry logic varies based on the overall wall-clock request time and Thrift CommandType
    being issued. ThriftBackend starts a timer and sets the current CommandType prior to
    initiating a network request. See `self.should_retry()` for details about what we do
    and do not retry.

    :param delay_min:
        Float of seconds for the minimum delay between retries. This is an alias for urllib3's
        `backoff_factor`.

    :param delay_max:
        Float of seconds for the maximum delay between retries.

    :param stop_after_attempts_count:
        Integer maximum number of attempts that will be retried. This is an alias for urllib3's
        `total`.

    :param stop_after_attempts_duration:
        Float of maximum number of seconds within which a request may be retried starting from
        the beginning of the first request.

    :param delay_default:
        Float of seconds the connector will wait between sucessive GetOperationStatus
        requests. This parameter is not used to retry failed network requests. We include
        it in this class to keep all retry behaviour encapsulated in this file.

    :param force_dangerous_codes:
        List of integer HTTP status codes that the connector will retry, even for dangerous
        commands like ExecuteStatement. This is passed to urllib3 by extending its status_forcelist

    :param urllib3_kwargs:
        Dictionary of arguments that are passed to Retry.__init__. Any setting of Retry() that
        Databricks does not override or extend may be modified here.
    """

    def __init__(
        self,
        delay_min: float,
        delay_max: float,
        stop_after_attempts_count: int,
        stop_after_attempts_duration: float,
        delay_default: float,
        force_dangerous_codes: List[int],
        urllib3_kwargs: dict = {},
    ):
        # These values do not change from one command to the next
        self.delay_max = delay_max
        self.delay_min = delay_min
        self.stop_after_attempts_count = stop_after_attempts_count
        self.stop_after_attempts_duration = stop_after_attempts_duration
        self._delay_default = delay_default
        self.force_dangerous_codes = force_dangerous_codes

        # the urllib3 kwargs are a mix of configuration (some of which we override)
        # and counters like `total` or `connect` which may change between successive retries
        # we only care about urllib3 kwargs that we alias, override, or add to in some way

        # the length of _history increases as retries are performed
        _history: Optional[Tuple[RequestHistory, ...]] = urllib3_kwargs.get("history")

        if not _history:
            # no attempts were made so we can retry the current command as many times as specified
            # by the user
            _attempts_remaining = self.stop_after_attempts_count
        else:
            # at least one of our attempts has been consumed, and urllib3 will have set a total
            # `total` is a counter that begins equal to self.stop_after_attempts_count and is
            # decremented after each unsuccessful request. When `total` is zero, urllib3 raises a
            # MaxRetryError
            _total: int = urllib3_kwargs.pop("total")
            _attempts_remaining = _total

        _urllib_kwargs_we_care_about = dict(
            total=_attempts_remaining,
            respect_retry_after_header=True,
            backoff_factor=self.delay_min,
            allowed_methods=["POST"],
            status_forcelist=[429, 503, *self.force_dangerous_codes],
        )

        urllib3_kwargs.update(**_urllib_kwargs_we_care_about)

        super().__init__(
            **urllib3_kwargs,  # type: ignore
        )

    @classmethod
    def __private_init__(
        cls, retry_start_time: float, command_type: Optional[CommandType], **init_kwargs
    ):
        """
        Returns a new instance of DatabricksRetryPolicy with the _retry_start_time and _command_type
        properties already set. This method should only be called by DatabricksRetryPolicy itself between
        successive Retry attempts.

        :param retry_start_time:
            Float unix timestamp. Used to monitor the overall request duration across successive
            retries. Never set this value directly. Use self.start_retry_timer() instead. Users
            never set this value. It is set by ThriftBackend immediately before issuing a network
            request.

        :param command_type:
            CommandType of the current request being retried. Used to modify retry behaviour based
            on the type of Thrift command being issued. See self.should_retry() for details. Users
            never set this value directly. It is set by ThriftBackend immediately before issuing
            a network request.

        :param init_kwargs:
            A dictionary of parameters that will be passed to __init__ in the new object
        """

        new_object = cls(**init_kwargs)
        new_object._retry_start_time = retry_start_time
        new_object.command_type = command_type
        return new_object

    def new(self, **urllib3_incremented_counters: typing.Any) -> Retry:
        """This method is responsible for passing the entire Retry state to its next iteration.

        urllib3 calls Retry.new() between successive requests as part of its `.increment()` method
        as shown below:

        ```python
            new_retry = self.new(
                total=total,
                connect=connect,
                read=read,
                redirect=redirect,
                status=status_count,
                other=other,
                history=history,
        )
        ```

        The arguments it passes to `.new()` (total, connect, read, etc.) are those modified by `.increment()`.

        Since self.__init__ has a different signature than Retry.__init__ , we implement our own `self.new()`
        to pipe our Databricks-specific state while preserving the super-class's behaviour.

        """

        # These arguments will match the function signature for self.__init__
        databricks_init_params = dict(
            delay_min=self.delay_min,
            delay_max=self.delay_max,
            stop_after_attempts_count=self.stop_after_attempts_count,
            stop_after_attempts_duration=self.stop_after_attempts_duration,
            delay_default=self.delay_default,
            force_dangerous_codes=self.force_dangerous_codes,
            urllib3_kwargs={},
        )

        # Gather urllib3's current retry state _before_ increment was called
        # These arguments match the function signature for Retry.__init__
        # Note: if we update urllib3 we may need to add/remove arguments from this dict
        urllib3_init_params = dict(
            total=self.total,
            connect=self.connect,
            read=self.read,
            redirect=self.redirect,
            status=self.status,
            other=self.other,
            allowed_methods=self.allowed_methods,
            status_forcelist=self.status_forcelist,
            backoff_factor=self.backoff_factor,  # type: ignore
            raise_on_redirect=self.raise_on_redirect,
            raise_on_status=self.raise_on_status,
            history=self.history,
            remove_headers_on_redirect=self.remove_headers_on_redirect,
            respect_retry_after_header=self.respect_retry_after_header,
        )

        # Update urllib3's current state to reflect the incremented counters
        urllib3_init_params.update(**urllib3_incremented_counters)

        # Include urllib3's current state in our __init__ params
        databricks_init_params["urllib3_kwargs"].update(**urllib3_init_params)  # type: ignore

        return type(self).__private_init__(
            retry_start_time=self._retry_start_time,
            command_type=self.command_type,
            **databricks_init_params,
        )

    @property
    def command_type(self) -> Optional[CommandType]:
        return self._command_type

    @command_type.setter
    def command_type(self, value: CommandType) -> None:
        self._command_type = value

    @property
    def delay_default(self) -> float:
        """Time in seconds the connector will wait between requests polling a GetOperationStatus Request

        This property is never read by urllib3 for the purpose of retries. It's stored in this class
        to keep all retry logic in one place.

        This property is only set by __init__ and cannot be modified afterward.
        """
        return self._delay_default

    def start_retry_timer(self) -> None:
        """Timer is used to monitor the overall time across successive requests

        Should only be called by ThriftBackend before sending a Thrift command"""
        self._retry_start_time = time.time()

    def check_timer_duration(self) -> float:
        """Return time in seconds since the timer was started"""

        if self._retry_start_time is None:
            raise OperationalError(
                "Cannot check retry timer. Timer was not started for this request."
            )
        else:
            return time.time() - self._retry_start_time

    def check_proposed_wait(self, proposed_wait: Union[int, float]) -> None:
        """Raise an exception if the proposed wait would exceed the configured max_attempts_duration"""

        proposed_overall_time = self.check_timer_duration() + proposed_wait
        if proposed_overall_time > self.stop_after_attempts_duration:
            raise MaxRetryDurationError(
                f"Retry request would exceed Retry policy max retry duration of {self.stop_after_attempts_duration} seconds"
            )

    def sleep_for_retry(self, response: BaseHTTPResponse) -> bool:  # type: ignore
        """Sleeps for the duration specified in the response Retry-After header, if present

        A MaxRetryDurationError will be raised if doing so would exceed self.max_attempts_duration

        This method is only called by urllib3 internals.
        """
        retry_after = self.get_retry_after(response)
        if retry_after:
            self.check_proposed_wait(retry_after)
            time.sleep(retry_after)
            return True

        return False

    def get_backoff_time(self) -> float:
        """Calls urllib3's built-in get_backoff_time.

        Never returns a value larger than self.delay_max
        A MaxRetryDurationError will be raised if the calculated backoff would exceed self.max_attempts_duration

        Note: within urllib3, a backoff is only calculated in cases where a Retry-After header is not present
            in the previous unsuccessful request and `self.respect_retry_after_header` is True (which is always true)
        """

        proposed_backoff = super().get_backoff_time()
        proposed_backoff = min(proposed_backoff, self.delay_max)
        self.check_proposed_wait(proposed_backoff)

        return proposed_backoff

    def should_retry(self, method: str, status_code: int) -> Tuple[bool, str]:
        """This method encapsulates the connector's approach to retries.

        We always retry a request unless one of these conditions is met:

            1. The request received a 200 (Success) status code
               Because the request succeeded .
            2. The request received a 501 (Not Implemented) status code
               Because this request can never succeed.
            3. The request received a 404 (Not Found) code and the request CommandType
               was CloseSession or CloseOperation. This code indicates that the session
               or cursor was already closed. Further retries will always return the same
               code.
            4. The request CommandType was ExecuteStatement and the HTTP code does not
               appear in the default status_forcelist or force_dangerous_codes list. By
               default, this means ExecuteStatement is only retried for codes 429 and 503.
               This limit prevents automatically retrying non-idempotent commands that could
               be destructive.


        Q: What about OSErrors and Redirects?
        A: urllib3 automatically retries in both scenarios

        Returns True if the request should be retried. Returns False or raises an exception
        if a retry would violate the configured policy.
        """

        # Request succeeded. Don't retry.
        if status_code == 200:
            return False, "200 codes are not retried"

        # Request failed and server said NotImplemented. This isn't recoverable. Don't retry.
        if status_code == 501:
            raise NonRecoverableNetworkError("Received code 501 from server.")

        # Request failed and this method is not retryable. We only retry POST requests.
        if not self._is_method_retryable(method):  # type: ignore
            return False, "Only POST requests are retried"

        # Request failed with 404 because CloseSession returns 404 if you repeat the request.
        if (
            status_code == 404
            and self.command_type == CommandType.CLOSE_SESSION
            and len(self.history) > 0
        ):
            raise SessionAlreadyClosedError(
                "CloseSession received 404 code from Databricks. Session is already closed."
            )

        # Request failed with 404 because CloseOperation returns 404 if you repeat the request.
        if (
            status_code == 404
            and self.command_type == CommandType.CLOSE_OPERATION
            and len(self.history) > 0
        ):
            raise CursorAlreadyClosedError(
                "CloseOperation received 404 code from Databricks. Cursor is already closed."
            )

        # Request failed, was an ExecuteStatement and the command may have reached the server
        if (
            self.command_type == CommandType.EXECUTE_STATEMENT
            and status_code not in self.status_forcelist
            and status_code not in self.force_dangerous_codes
        ):
            raise UnsafeToRetryError(
                "ExecuteStatement command can only be retried for codes 429 and 503"
            )

        # Request failed with a dangerous code, was an ExecuteStatement, but user forced retries for this
        # dangerous code. Note that these lines _are not required_ to make these requests retry. They would
        # retry automatically. This code is included only so that we can log the exact reason for the retry.
        # This gives users signal that their _retry_dangerous_codes setting actually did something.
        if (
            self.command_type == CommandType.EXECUTE_STATEMENT
            and status_code in self.force_dangerous_codes
        ):
            return (
                True,
                f"Request failed with dangerous code {status_code} that is one of the configured _retry_dangerous_codes.",
            )

        # None of the above conditions applied. Eagerly retry.
        logger.debug(
            f"This request should be retried: {self.command_type and self.command_type.value}"
        )
        return (
            True,
            "Failed requests are retried by default per configured DatabricksRetryPolicy",
        )

    def is_retry(
        self, method: str, status_code: int, has_retry_after: bool = False
    ) -> bool:
        """
        Called by urllib3 when determining whether or not to retry

        Logs a debug message if the request will be retried
        """

        should_retry, msg = self.should_retry(method, status_code)

        if should_retry:
            logger.debug(msg)

        return should_retry
